package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"zeam/identity"
	"zeam/pong"

	"github.com/ethereum/go-ethereum/common"
)

func newUnifiedIdentity(dataDir string, secret []byte) (*identity.UnifiedIdentity, error) {
	return identity.NewUnifiedIdentity(identity.UnifiedConfig{
		Mode:       identity.ModeStateless,
		DataDir:    dataDir,
		UserSecret: secret,
	})
}

func newLegacyIdentity(u *identity.UnifiedIdentity) *identity.Identity {
	return &identity.Identity{
		PrivateKey: u.PrivateKey,
		Address:    u.Address,
		ForkID:     u.ForkID,
		ShortID:    u.ShortID,
	}
}

type IpcRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type IpcResponse struct {
	ID     uint64      `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type IpcEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type IpcServer struct {
	app        *App
	listener   net.Listener
	socketPath string
	noP2P      bool
	p2pPeer    string

	mu          sync.Mutex
	subscribers map[net.Conn]map[string]bool
}

func StartIpcServer(app *App, socketPath string, noP2P bool, p2pPeer string) (*IpcServer, error) {
	listener, err := ipcListen(socketPath)
	if err != nil {
		return nil, err
	}

	s := &IpcServer{
		app:         app,
		listener:    listener,
		socketPath:  socketPath,
		noP2P:       noP2P,
		p2pPeer:     p2pPeer,
		subscribers: make(map[net.Conn]map[string]bool),
	}

	log.Printf("[IPC] Listening on %s", socketPath)

	go s.acceptLoop()
	go s.flowBroadcastLoop()

	return s, nil
}

func (s *IpcServer) Close() {
	s.listener.Close()
	ipcCleanup(s.socketPath)
}

func (s *IpcServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			log.Printf("[IPC] Accept error: %v", err)
			return
		}
		go s.handleConn(conn)
	}
}

func (s *IpcServer) handleConn(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	for {
		req, err := readIpcFrame(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("[IPC] Read error: %v", err)
			}
			return
		}

		result, errStr := s.dispatch(req, conn)
		resp := IpcResponse{ID: req.ID}
		if errStr != "" {
			resp.Error = errStr
		} else {
			resp.Result = result
		}

		if err := writeIpcFrame(conn, resp); err != nil {
			log.Printf("[IPC] Write error: %v", err)
			return
		}
	}
}

func (s *IpcServer) dispatch(req *IpcRequest, conn net.Conn) (interface{}, string) {
	switch req.Method {

	case "auth/status":
		s.app.mu.RLock()
		unlocked := s.app.unlocked
		s.app.mu.RUnlock()
		chainSaltExists := fileExists(s.app.dataDir + "/chain_salt.anchor")
		return map[string]interface{}{
			"needs_passkey":  !unlocked,
			"is_new":         !chainSaltExists,
			"stateless_mode": true,
			"has_chain_salt": chainSaltExists,
		}, ""

	case "auth/unlock":
		var params struct {
			Passkey string `json:"passkey"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, "invalid params: " + err.Error()
		}
		return s.handleUnlock(params.Passkey)

	case "health":
		return s.handleHealth(), ""

	case "pressure":
		if s.app.backend == nil {
			return map[string]interface{}{
				"hadamard": 0, "pauliX": 0, "pauliZ": 0, "phase": 0,
			}, ""
		}
		p := s.app.backend.GetPressure()
		return map[string]interface{}{
			"hadamard": p.Hadamard,
			"pauliX":   p.PauliX,
			"pauliZ":   p.PauliZ,
			"phase":    p.Phase,
		}, ""

	case "collapse":
		var params struct {
			Prompt    string `json:"prompt"`
			Broadcast bool   `json:"broadcast"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, "invalid params: " + err.Error()
		}
		return s.handleCollapse(params.Prompt, params.Broadcast)

	case "pong/state":
		return s.handlePongState(), ""

	case "pong/start":
		return s.handlePongStart()

	case "pong/stop":
		s.app.mu.Lock()
		if s.app.pongEngine != nil {
			s.app.pongEngine.Stop()
		}
		s.app.mu.Unlock()
		return map[string]interface{}{"status": "stopped"}, ""

	case "pong/reset":
		s.app.mu.Lock()
		if s.app.pongEngine != nil {
			s.app.pongEngine.Reset()
		}
		s.app.mu.Unlock()
		return map[string]interface{}{"status": "reset"}, ""

	case "pong/input":
		var params struct {
			Player int    `json:"player"`
			Input  string `json:"input"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, "invalid params"
		}
		var input pong.Input
		switch params.Input {
		case "up":
			input = pong.InputUp
		case "down":
			input = pong.InputDown
		default:
			input = pong.InputNone
		}
		s.app.mu.RLock()
		engine := s.app.pongEngine
		s.app.mu.RUnlock()
		if engine != nil {
			engine.SetInput(uint8(params.Player), input)
		}
		return map[string]interface{}{"ok": true}, ""

	case "arb/stats":
		return s.handleArbStats(), ""

	case "arb/routes":
		return s.handleArbRoutes(), ""

	case "subscribe":
		var params struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, "invalid params"
		}
		s.mu.Lock()
		if s.subscribers[conn] == nil {
			s.subscribers[conn] = make(map[string]bool)
		}
		s.subscribers[conn][params.Channel] = true
		s.mu.Unlock()
		return map[string]interface{}{"subscribed": params.Channel}, ""

	default:
		return nil, "unknown method: " + req.Method
	}
}

func (s *IpcServer) handleUnlock(passkey string) (interface{}, string) {
	s.app.mu.Lock()
	defer s.app.mu.Unlock()

	if s.app.unlocked {
		return map[string]interface{}{"success": true}, ""
	}

	passkeySecret := []byte(passkey)
	log.Printf("[IPC] Deriving identity from secret (len=%d)", len(passkeySecret))

	unifiedID, err := newUnifiedIdentity(s.app.dataDir, passkeySecret)

	for i := range passkeySecret {
		passkeySecret[i] = 0
	}

	if err != nil {
		return map[string]interface{}{"error": err.Error()}, ""
	}

	log.Printf("[IPC] Identity derived: %s", unifiedID.Address.Hex())
	s.app.unifiedID = unifiedID
	s.app.id = newLegacyIdentity(unifiedID)
	s.app.unlocked = true

	if !s.noP2P {
		go s.app.startP2P(s.p2pPeer)
	}

	return map[string]interface{}{
		"success":        true,
		"stateless_mode": true,
		"address":        s.app.id.Address.Hex(),
	}, ""
}

func (s *IpcServer) handleHealth() interface{} {
	s.app.mu.RLock()
	defer s.app.mu.RUnlock()

	resp := map[string]interface{}{
		"status":          "ok",
		"unlocked":        s.app.unlocked,
		"stateless_mode":  true,
		"hardware_gate":   false,
		"words":           uint64(0),
		"fork_id":         "",
		"multichain_ready": s.app.multiChain != nil,
	}

	if s.app.backend != nil {
		resp["words"] = s.app.backend.WordCount()
	}
	if s.app.id != nil {
		resp["fork_id"] = s.app.id.ForkID
	}
	return resp
}

func (s *IpcServer) handleCollapse(prompt string, broadcast bool) (interface{}, string) {
	s.app.mu.RLock()
	backend := s.app.backend
	s.app.mu.RUnlock()

	if backend == nil {
		return nil, "backend not initialized"
	}

	if !backend.IsCollapseEnabled() {
		return nil, "collapse not enabled - unlock identity first"
	}

	response, txHash, err := backend.CollapseGenerate(prompt)
	if err != nil {
		return nil, err.Error()
	}

	s.app.mu.RLock()
	forkID := ""
	if s.app.id != nil {
		forkID = fmt.Sprintf("%x", s.app.id.ForkID)
	}
	s.app.mu.RUnlock()

	pressure := backend.GetPressure()
	l1Peers, relays, blockAge := backend.GetNetworkState()

	result := map[string]interface{}{
		"response":  response,
		"tx_hashes": []string{txHash.Hex()},
		"fork_id":   forkID,
		"pressure": map[string]interface{}{
			"hadamard": pressure.Hadamard,
			"pauliX":   pressure.PauliX,
			"pauliZ":   pressure.PauliZ,
			"phase":    pressure.Phase,
		},
		"network": map[string]interface{}{
			"peers":     l1Peers,
			"relays":    relays,
			"block_age": blockAge,
		},
	}

	if semanticPath := backend.GetSemanticPath(); semanticPath != nil {
		result["semantic_path"] = semanticPath
	}

	if broadcast {
		s.app.mu.RLock()
		p2p := s.app.p2pTransport
		id := s.app.id
		s.app.mu.RUnlock()

		if p2p != nil && id != nil {
			queryHash := sha256.Sum256([]byte(prompt))
			hash, err := p2p.BroadcastCollapseInput(queryHash, []byte(prompt))
			if err != nil {
				result["broadcast_error"] = err.Error()
			} else {
				result["broadcast_tx"] = hash.Hex()
			}
		}
	}

	return result, ""
}

func (s *IpcServer) handlePongState() interface{} {
	s.app.mu.RLock()
	engine := s.app.pongEngine
	s.app.mu.RUnlock()

	if engine == nil {
		return map[string]interface{}{"running": false}
	}

	state := engine.State()
	return map[string]interface{}{
		"running":   true,
		"tick":      state.Tick,
		"ball_x":    state.BallX,
		"ball_y":    state.BallY,
		"ball_dx":   state.BallDX,
		"ball_dy":   state.BallDY,
		"paddle1_y": state.Paddle1Y,
		"paddle2_y": state.Paddle2Y,
		"score1":    state.Score1,
		"score2":    state.Score2,
		"game_over": state.GameOver,
		"winner":    state.Winner,
		"field": map[string]int{
			"width":         pong.FieldWidth,
			"height":        pong.FieldHeight,
			"paddle_height": pong.PaddleHeight,
			"paddle_width":  pong.PaddleWidth,
			"ball_size":     pong.BallSize,
		},
	}
}

func (s *IpcServer) handlePongStart() (interface{}, string) {
	s.app.mu.Lock()
	defer s.app.mu.Unlock()

	if s.app.pongEngine != nil {
		s.app.pongEngine.Stop()
	}

	computeHash := func(payload []byte) (common.Hash, error) {
		if s.app.multiChain == nil {
			return common.Hash{}, fmt.Errorf("network not connected")
		}
		flowEntropy := s.app.multiChain.GetUnifiedEntropy()
		if len(flowEntropy) == 0 {
			return common.Hash{}, fmt.Errorf("no flow entropy available")
		}
		combined := make([]byte, 32)
		copy(combined, flowEntropy)
		for i := 0; i < len(payload) && i < 32; i++ {
			combined[i] ^= payload[i]
		}
		return common.BytesToHash(combined), nil
	}

	flowCompute := s.app.multiChain.GetFlowCompute()
	flowProvider := &pongFlowAdapter{flow: flowCompute}

	s.app.pongEngine = pong.NewAdversaryEngine(computeHash, flowProvider)
	s.app.pongEngine.SetTickRate(50 * time.Millisecond)
	s.app.pongEngine.SetAdversaryDifficulty(0.6)
	s.app.pongEngine.OnScore = func(player, s1, s2 uint8) {
		log.Printf("[Pong] Player %d scores! %d-%d", player, s1, s2)
	}
	s.app.pongEngine.OnGameOver = func(winner uint8) {
		log.Printf("[Pong] Game over! Player %d wins!", winner)
	}
	s.app.pongEngine.Start()

	return map[string]interface{}{"status": "started"}, ""
}

func (s *IpcServer) handleArbStats() interface{} {
	s.app.mu.RLock()
	defer s.app.mu.RUnlock()

	if s.app.arbDetector == nil {
		return map[string]interface{}{
			"pools_discovered":   0,
			"graph_nodes":        0,
			"graph_edges":        0,
			"routes_found":       0,
			"routes_explored":    0,
			"best_profit_bps":    0.0,
			"txs_processed":      0,
			"total_pressure":     0.0,
			"pressure_collapses": 0,
		}
	}

	stats := s.app.arbDetector.Stats()
	neuralStats := s.app.arbDetector.NeuralStats()

	routesExplored := 0
	pressureCollapses := 0
	totalPressure := 0.0

	if nf, ok := neuralStats["neural_finder"].(map[string]interface{}); ok {
		if v, ok := nf["routes_explored"].(uint64); ok {
			routesExplored = int(v)
		}
		if v, ok := nf["pressure_collapses"].(uint64); ok {
			pressureCollapses = int(v)
		}
		if v, ok := nf["total_pressure"].(float64); ok {
			totalPressure = v
		}
	}
	if tp, ok := neuralStats["total_pressure"].(float64); ok {
		totalPressure = tp
	}

	return map[string]interface{}{
		"pools_discovered":   stats.PoolsDiscovered,
		"graph_nodes":        stats.GraphNodes,
		"graph_edges":        stats.GraphEdges,
		"routes_found":       stats.RoutesFound,
		"best_profit_bps":    stats.BestProfitBPS,
		"routes_explored":    routesExplored,
		"pressure_collapses": pressureCollapses,
		"total_pressure":     totalPressure,
		"txs_processed":      stats.TxsProcessed,
	}
}

func (s *IpcServer) handleArbRoutes() interface{} {
	s.app.mu.RLock()
	defer s.app.mu.RUnlock()

	if s.app.arbDetector == nil {
		return map[string]interface{}{"routes": []interface{}{}}
	}

	routes := s.app.arbDetector.GetRecentRoutes(20)
	routeList := make([]map[string]interface{}, 0, len(routes))
	for _, route := range routes {
		hops := make([]map[string]interface{}, 0, len(route.Hops))
		for _, hop := range route.Hops {
			hops = append(hops, map[string]interface{}{
				"pool":      hop.Pool.Hex(),
				"dex":       hop.DEX,
				"token_in":  hop.TokenIn.Hex(),
				"token_out": hop.TokenOut.Hex(),
				"chain_id":  hop.ChainID,
			})
		}
		idHex := ""
		for _, b := range route.ID[:8] {
			idHex += fmt.Sprintf("%02x", b)
		}
		routeList = append(routeList, map[string]interface{}{
			"id":             idHex,
			"profit_bps":     route.ProfitBPS,
			"start_chain":    route.StartChain,
			"start_token":    route.StartToken.Hex(),
			"hops":           hops,
			"is_cross_chain": route.IsCrossChain,
		})
	}

	return map[string]interface{}{"routes": routeList}
}

func (s *IpcServer) flowBroadcastLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		var flowConns []net.Conn
		for conn, channels := range s.subscribers {
			if channels["flow"] {
				flowConns = append(flowConns, conn)
			}
		}
		s.mu.Unlock()

		if len(flowConns) == 0 {
			continue
		}

		s.app.mu.RLock()
		backend := s.app.backend
		s.app.mu.RUnlock()

		if backend == nil {
			continue
		}

		pressure := backend.GetPressure()
		l1Peers, relays, blockAge := backend.GetNetworkState()
		dynamics := backend.GetFlowDynamics()

		evt := IpcEvent{
			Event: "flow",
			Data: map[string]interface{}{
				"type": "flow_update",
				"pressure": map[string]float64{
					"hadamard": pressure.Hadamard,
					"pauliX":   pressure.PauliX,
					"pauliZ":   pressure.PauliZ,
					"phase":    pressure.Phase,
				},
				"dynamics": map[string]float64{
					"pressure":  dynamics.Pressure,
					"tension":   dynamics.Tension,
					"rhythm":    dynamics.Rhythm,
					"coherence": dynamics.Coherence,
				},
				"network": map[string]interface{}{
					"peers":     l1Peers,
					"relays":    relays,
					"block_age": blockAge,
				},
				"timestamp": time.Now().UnixMilli(),
			},
		}

		for _, conn := range flowConns {
			if err := writeIpcFrame(conn, evt); err != nil {
				s.mu.Lock()
				delete(s.subscribers, conn)
				s.mu.Unlock()
				conn.Close()
			}
		}
	}
}

func readIpcFrame(r io.Reader) (*IpcRequest, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.LittleEndian.Uint32(lenBuf[:])
	if length > 16*1024*1024 {
		return nil, fmt.Errorf("frame too large: %d", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	var req IpcRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func writeIpcFrame(w io.Writer, v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}

	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}
