package arb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
)

type FlashblocksClient struct {
	mu sync.RWMutex

	url  string
	conn *websocket.Conn

	latest *FlashblockData

	priceFeed *PriceFeed

	OnFlashblock func(*FlashblockData)
	OnSwap       func(*SwapEvent)
	OnError      func(error)

	config *FlashblocksConfig

	totalBlocks uint64
	totalSwaps  uint64
	lastBlock   time.Time

	running    bool
	stopCh     chan struct{}
	reconnectCh chan struct{}
	wg         sync.WaitGroup
}

type FlashblocksConfig struct {
	URL               string
	ReconnectInterval time.Duration
	MaxReconnects     int
	PingInterval      time.Duration
	ReadTimeout       time.Duration
}

func DefaultFlashblocksConfig() *FlashblocksConfig {
	return &FlashblocksConfig{
		URL:               "wss://mainnet.flashblocks.base.org/ws",
		ReconnectInterval: 5 * time.Second,
		MaxReconnects:     10,
		PingInterval:      30 * time.Second,
		ReadTimeout:       60 * time.Second,
	}
}

func NewFlashblocksClient(priceFeed *PriceFeed, config *FlashblocksConfig) *FlashblocksClient {
	if config == nil {
		config = DefaultFlashblocksConfig()
	}

	return &FlashblocksClient{
		url:         config.URL,
		priceFeed:   priceFeed,
		config:      config,
		stopCh:      make(chan struct{}),
		reconnectCh: make(chan struct{}, 1),
	}
}

func (fc *FlashblocksClient) Start() error {
	fc.mu.Lock()
	if fc.running {
		fc.mu.Unlock()
		return nil
	}
	fc.running = true
	fc.mu.Unlock()

	if err := fc.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	fc.wg.Add(1)
	go fc.readLoop()

	fc.wg.Add(1)
	go fc.pingLoop()

	fc.wg.Add(1)
	go fc.reconnectLoop()

	return nil
}

func (fc *FlashblocksClient) Stop() {
	fc.mu.Lock()
	if !fc.running {
		fc.mu.Unlock()
		return
	}
	fc.running = false
	close(fc.stopCh)
	fc.mu.Unlock()

	if fc.conn != nil {
		fc.conn.Close()
	}

	fc.wg.Wait()
}

func (fc *FlashblocksClient) connect() error {
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(fc.url, nil)
	if err != nil {
		return err
	}

	fc.mu.Lock()
	fc.conn = conn
	fc.mu.Unlock()

	subscribeMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []string{"flashblocks"},
	}

	if err := conn.WriteJSON(subscribeMsg); err != nil {
		conn.Close()
		return fmt.Errorf("subscribe failed: %w", err)
	}

	log.Printf("[Flashblocks] Connected to %s - 200ms preconfirmations active", fc.url)
	return nil
}

func (fc *FlashblocksClient) readLoop() {
	defer fc.wg.Done()

	for {
		select {
		case <-fc.stopCh:
			return
		default:
		}

		fc.mu.RLock()
		conn := fc.conn
		fc.mu.RUnlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(fc.config.ReadTimeout))

		_, message, err := conn.ReadMessage()
		if err != nil {
			if fc.OnError != nil {
				fc.OnError(fmt.Errorf("read error: %w", err))
			}
			fc.triggerReconnect()
			continue
		}

		fc.processMessage(message)
	}
}

type FlashblockUpdate struct {
	PayloadID string          `json:"payload_id"`
	Index     int             `json:"index"`
	Base      json.RawMessage `json:"base,omitempty"`
	Diff      json.RawMessage `json:"diff,omitempty"`
}

func (fc *FlashblocksClient) processMessage(data []byte) {

	reader := brotli.NewReader(bytes.NewReader(data))
	decompressed, err := io.ReadAll(reader)
	if err != nil {

		decompressed = data
	}

	var update FlashblockUpdate
	if err := json.Unmarshal(decompressed, &update); err != nil {

		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(decompressed, &msg); err != nil {
			return
		}

		return
	}

	fc.mu.Lock()
	fc.totalBlocks++
	fc.lastBlock = time.Now()
	fc.mu.Unlock()

	if update.Index == 0 {
		log.Printf("[Flashblocks] New block %s - 200ms preconf stream active", update.PayloadID)
	}

	if fc.OnFlashblock != nil {
		go fc.OnFlashblock(&FlashblockData{
			Slot:      fc.totalBlocks,
			Timestamp: time.Now(),
			ValidUntil: time.Now().Add(200 * time.Millisecond),
		})
	}

}

type FlashblockMessage struct {
	Slot         uint64   `json:"slot"`
	Timestamp    uint64   `json:"timestamp"`
	Builder      string   `json:"builder"`
	Transactions []string `json:"transactions"`

	Swaps []struct {
		Pool      string `json:"pool"`
		TokenIn   string `json:"tokenIn"`
		TokenOut  string `json:"tokenOut"`
		AmountIn  string `json:"amountIn"`
		AmountOut string `json:"amountOut"`
		TxHash    string `json:"txHash"`
	} `json:"swaps"`
}

func (fc *FlashblocksClient) convertFlashblock(msg *FlashblockMessage) *FlashblockData {
	fb := &FlashblockData{
		Slot:         msg.Slot,
		Timestamp:    time.Unix(int64(msg.Timestamp), 0),
		BlockBuilder: msg.Builder,
		ValidUntil:   time.Now().Add(200 * time.Millisecond),
		Transactions: make([]common.Hash, len(msg.Transactions)),
		SwapEvents:   make([]SwapEvent, 0, len(msg.Swaps)),
	}

	for i, txHash := range msg.Transactions {
		fb.Transactions[i] = common.HexToHash(txHash)
	}

	for _, swap := range msg.Swaps {
		amountIn, _ := new(big.Int).SetString(swap.AmountIn, 10)
		amountOut, _ := new(big.Int).SetString(swap.AmountOut, 10)

		se := SwapEvent{
			ChainID:   ChainIDBase,
			Pool:      common.HexToAddress(swap.Pool),
			TxHash:    common.HexToHash(swap.TxHash),
			Timestamp: fb.Timestamp,
			Source:    PriceSourceFlashblock,
		}

		tokenIn := common.HexToAddress(swap.TokenIn)
		tokenOut := common.HexToAddress(swap.TokenOut)

		if amountIn != nil {
			se.Token0In = amountIn
			se.Token1Out = amountOut
		}
		if tokenIn != (common.Address{}) && tokenOut != (common.Address{}) {
			fb.SwapEvents = append(fb.SwapEvents, se)
		}
	}

	return fb
}

func (fc *FlashblocksClient) pingLoop() {
	defer fc.wg.Done()

	ticker := time.NewTicker(fc.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-fc.stopCh:
			return
		case <-ticker.C:
			fc.mu.RLock()
			conn := fc.conn
			fc.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					fc.triggerReconnect()
				}
			}
		}
	}
}

func (fc *FlashblocksClient) reconnectLoop() {
	defer fc.wg.Done()

	reconnects := 0

	for {
		select {
		case <-fc.stopCh:
			return
		case <-fc.reconnectCh:
			reconnects++
			if reconnects > fc.config.MaxReconnects {
				if fc.OnError != nil {
					fc.OnError(fmt.Errorf("max reconnects exceeded"))
				}
				return
			}

			fc.mu.Lock()
			if fc.conn != nil {
				fc.conn.Close()
				fc.conn = nil
			}
			fc.mu.Unlock()

			select {
			case <-fc.stopCh:
				return
			case <-time.After(fc.config.ReconnectInterval):
			}

			if err := fc.connect(); err != nil {
				if fc.OnError != nil {
					fc.OnError(fmt.Errorf("reconnect failed: %w", err))
				}
				fc.triggerReconnect()
			} else {
				reconnects = 0
			}
		}
	}
}

func (fc *FlashblocksClient) triggerReconnect() {
	select {
	case fc.reconnectCh <- struct{}{}:
	default:
	}
}

func (fc *FlashblocksClient) GetLatest() *FlashblockData {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.latest
}

func (fc *FlashblocksClient) IsConnected() bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.conn != nil
}

func (fc *FlashblocksClient) GetStats() FlashblocksStats {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	return FlashblocksStats{
		TotalBlocks: fc.totalBlocks,
		TotalSwaps:  fc.totalSwaps,
		LastBlock:   fc.lastBlock,
		Connected:   fc.conn != nil,
		URL:         fc.url,
	}
}

type FlashblocksStats struct {
	TotalBlocks uint64
	TotalSwaps  uint64
	LastBlock   time.Time
	Connected   bool
	URL         string
}

func (fc *FlashblocksClient) ValidatePrediction(forecast *PoolImpactForecast) bool {
	fc.mu.RLock()
	fb := fc.latest
	fc.mu.RUnlock()

	if fb == nil || forecast == nil {
		return false
	}

	if time.Now().After(fb.ValidUntil) {
		return false
	}

	for _, swap := range fb.SwapEvents {
		if swap.Pool == forecast.Pool && swap.ChainID == forecast.ChainID {
			return true
		}
	}
	return false
}

func (fc *FlashblocksClient) SetURL(url string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.url = url
}
