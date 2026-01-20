

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"zeam/identity"
	"zeam/llm"
	"zeam/node"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/term"
)


type App struct {
	mu              sync.RWMutex
	id              *identity.Identity        
	unifiedID       *identity.UnifiedIdentity 
	backend         *llm.NGACBackend
	p2pTransport    *node.P2PTransport
	hybridTransport *node.HybridTransport
	dataDir         string
	oewnPath        string
	p2pListen       string
	useRelay        bool 
	unlocked        bool
	statelessMode   bool 
}


type liveDispatcherAdapter struct {
	lc *node.LiveCompute
}

func (a *liveDispatcherAdapter) Dispatch(ctx context.Context, payload []byte) (*llm.DispatchResult, error) {
	result, err := a.lc.Dispatch(ctx, payload)
	if err != nil {
		return nil, err
	}
	return &llm.DispatchResult{
		Hashes:    result.Hashes,
		HashBytes: result.HashBytes,
		Duration:  result.Duration,
	}, nil
}

func (a *liveDispatcherAdapter) SetTimeout(timeout time.Duration) {
	a.lc.SetTimeout(timeout)
}


type flowCognitionAdapter struct {
	compute   *node.FlowCompute
	cognition *node.ActiveCognition
}


type unifiedCognitionAdapter struct {
	unified     *node.UnifiedCognition
	flowCompute *node.UnifiedFlowCompute
}


func (f *flowCognitionAdapter) NextBytes(n int) []byte {
	if f.compute == nil {
		return make([]byte, n)
	}
	return f.compute.NextBytes(n)
}

func (f *flowCognitionAdapter) NextUint64() uint64 {
	if f.compute == nil {
		return 0
	}
	return f.compute.NextUint64()
}

func (f *flowCognitionAdapter) SelectIndex(n int) int {
	if f.compute == nil || n <= 0 {
		return 0
	}
	return f.compute.SelectIndex(n)
}


func (f *flowCognitionAdapter) Inject(inj llm.FlowInjection) {
	if f.cognition == nil {
		return
	}
	f.cognition.Inject(node.Injection{
		Type:     node.InjectionType(inj.Type),
		Content:  inj.Content,
		Concepts: inj.Concepts,
		Strength: inj.Strength,
	})
}

func (f *flowCognitionAdapter) Query(content string, concepts []string) {
	if f.cognition != nil {
		f.cognition.Query(content, concepts)
	}
}

func (f *flowCognitionAdapter) Steer(concepts []string, strength float64) {
	if f.cognition != nil {
		f.cognition.Steer(concepts, strength)
	}
}

func (f *flowCognitionAdapter) Focus(concepts []string) {
	if f.cognition != nil {
		f.cognition.Focus(concepts)
	}
}

func (f *flowCognitionAdapter) State() llm.FlowCognitiveState {
	if f.cognition == nil {
		return llm.FlowCognitiveState{}
	}
	state := f.cognition.State()
	return llm.FlowCognitiveState{
		ActiveConcepts: state.ActiveConcepts,
		ConceptWeights: state.ConceptWeights,
		ReadyToRespond: state.ReadyToRespond,
	}
}

func (f *flowCognitionAdapter) Dynamics() llm.FlowDynamicsState {
	if f.cognition == nil {
		return llm.FlowDynamicsState{Coherence: 0.5}
	}
	d := f.cognition.Dynamics()
	return llm.FlowDynamicsState{
		Pressure:  d.Pressure,
		Tension:   d.Tension,
		Rhythm:    d.Rhythm,
		Coherence: d.Coherence,
	}
}


func (u *unifiedCognitionAdapter) NextBytes(n int) []byte {
	if u.flowCompute == nil {
		return make([]byte, n)
	}
	return u.flowCompute.NextBytes(n)
}

func (u *unifiedCognitionAdapter) NextUint64() uint64 {
	if u.flowCompute == nil {
		return 0
	}
	return u.flowCompute.NextUint64()
}

func (u *unifiedCognitionAdapter) SelectIndex(n int) int {
	if u.flowCompute == nil || n <= 0 {
		return 0
	}
	return u.flowCompute.SelectIndex(n)
}


func (u *unifiedCognitionAdapter) Inject(inj llm.FlowInjection) {
	if u.unified == nil {
		return
	}
	
	switch inj.Type {
	case 0: 
		u.unified.InjectUserInput(inj.Content, inj.Concepts)
	case 1: 
		u.unified.Steer(inj.Concepts, inj.Strength)
	default:
		u.unified.InjectUserInput(inj.Content, inj.Concepts)
	}
}

func (u *unifiedCognitionAdapter) Query(content string, concepts []string) {
	if u.unified != nil {
		u.unified.Query(content, concepts, nil)
	}
}

func (u *unifiedCognitionAdapter) Steer(concepts []string, strength float64) {
	if u.unified != nil {
		u.unified.Steer(concepts, strength)
	}
}

func (u *unifiedCognitionAdapter) Focus(concepts []string) {
	if u.unified != nil {
		
		u.unified.Steer(concepts, 0.8)
	}
}

func (u *unifiedCognitionAdapter) State() llm.FlowCognitiveState {
	if u.unified == nil {
		return llm.FlowCognitiveState{}
	}
	concepts := u.unified.ActiveConcepts(10)
	weights := make(map[string]float64)
	for _, c := range concepts {
		weights[c] = u.unified.ConceptWeight(c)
	}
	dynamics := u.unified.Dynamics()
	return llm.FlowCognitiveState{
		ActiveConcepts: concepts,
		ConceptWeights: weights,
		ReadyToRespond: dynamics.ReadyToAct,
	}
}

func (u *unifiedCognitionAdapter) Dynamics() llm.FlowDynamicsState {
	if u.unified == nil {
		return llm.FlowDynamicsState{Coherence: 0.5}
	}
	d := u.unified.Dynamics()
	return llm.FlowDynamicsState{
		Pressure:  d.FlowPressure,
		Tension:   d.FlowTension,
		Rhythm:    d.FlowRhythm,
		Coherence: d.FlowCoherence,
	}
}

func main() {
	
	oewnPath := flag.String("oewn", "oewn.json", "Path to OEWN JSON file")
	port := flag.String("port", "8080", "HTTP server port")
	dataDir := flag.String("data", "", "Data directory (default: ~/.zeam)")
	uiDir := flag.String("ui", "", "UI directory to serve (optional)")
	importKey := flag.String("import-key", "", "Import existing private key (hex)")
	exportKey := flag.Bool("export-key", false, "Export private key and exit")
	cliMode := flag.Bool("cli", false, "Run in CLI mode (terminal passkey prompt)")

	
	usePasskey := flag.Bool("passkey", false, "Use passkey to encrypt identity key (CLI mode)")
	newIdentity := flag.Bool("new", false, "Create new identity (with passkey if -passkey set)")
	showRecovery := flag.Bool("show-recovery", false, "Show recovery phrase for backup")

	
	p2pListen := flag.String("p2p-listen", ":30313", "P2P listen address")
	p2pPeer := flag.String("p2p-peer", "", "Bootstrap peer enode URL (e.g., enode://...@127.0.0.1:30305)")
	useRelay := flag.Bool("relay", true, "Use libp2p/IPFS relay for NAT traversal (recommended)")

	flag.Parse()

	
	resolvedDataDir := *dataDir
	if resolvedDataDir == "" {
		resolvedDataDir = identity.DefaultDataDir()
	}

	
	os.MkdirAll(resolvedDataDir, 0700)
	logFile, _ := os.OpenFile(resolvedDataDir+"/zeam.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if logFile != nil {
		
		log.SetOutput(logFile)
		log.SetFlags(log.Ltime)
	}

	log.Println("===========")
	log.Println("   ZEAM")
	log.Println("===========")
	log.Printf("[ZEAM] Data directory: %s", resolvedDataDir)

	
	chainSaltExists := fileExists(resolvedDataDir + "/chain_salt.anchor")
	if chainSaltExists {
		log.Println("[ZEAM] Mode: Stateless (existing identity)")
	} else {
		log.Println("[ZEAM] Mode: Stateless (new user)")
	}

	app := &App{
		dataDir:       resolvedDataDir,
		oewnPath:      *oewnPath,
		p2pListen:     *p2pListen,
		useRelay:      *useRelay,
		statelessMode: true, 
	}

	
	if *cliMode || *exportKey || *importKey != "" {
		var passkeySecret []byte

		
		if chainSaltExists {
			fmt.Println("[ZEAM] Stateless identity found. Enter user secret to derive.")
			passkeySecret = readPasskey("User Secret: ")
		} else {
			fmt.Println("[ZEAM] Creating new stateless identity...")
			passkeySecret = readPasskey("Create User Secret (min 8 chars): ")
			if len(passkeySecret) < 8 {
				log.Fatal("[ZEAM] User secret must be at least 8 characters")
			}
		}

		unifiedID, err := identity.NewUnifiedIdentity(identity.UnifiedConfig{
			Mode:       identity.ModeStateless,
			DataDir:    resolvedDataDir,
			UserSecret: passkeySecret,
		})

		for i := range passkeySecret {
			passkeySecret[i] = 0
		}

		if err != nil {
			log.Fatalf("[ZEAM] Failed to derive identity: %v", err)
		}

		app.unifiedID = unifiedID
		app.id = &identity.Identity{
			PrivateKey: unifiedID.PrivateKey,
			Address:    unifiedID.Address,
			ForkID:     unifiedID.ForkID,
			ShortID:    unifiedID.ShortID,
		}
		app.unlocked = true

		fmt.Printf("[ZEAM] Stateless identity derived\n")
		fmt.Printf("[ZEAM] Hardware gate: %s\n", unifiedID.GateType())
		fmt.Printf("[ZEAM] Address: %s\n", unifiedID.Address.Hex())

		if *exportKey {
			fmt.Println("[ZEAM] Note: Stateless identity cannot be exported - re-derive with user secret")
			return
		}

		
		_ = newIdentity
		_ = usePasskey
		_ = showRecovery
		_ = importKey
	}

	
	fmt.Printf("[ZEAM] Loading NGAC with OEWN from %s...\n", *oewnPath)
	backend, err := llm.NewNGACBackend(*oewnPath)
	if err != nil {
		log.Fatalf("[ZEAM] Failed to initialize NGAC: %v", err)
	}
	app.backend = backend
	fmt.Printf("[ZEAM] NGAC initialized: %d words\n", backend.WordCount())

	
	if err := backend.EnableHashNetwork(); err != nil {
		fmt.Printf("[ZEAM] Warning: HashNetwork not enabled: %v\n", err)
	} else {
		fmt.Println("[ZEAM] HashNetwork enabled (hash-native tensor operations)")
	}
	fmt.Println()

	
	mux := http.NewServeMux()

	cors := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	
	mux.HandleFunc("/auth/status", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		unlocked := app.unlocked
		app.mu.RUnlock()

		chainSaltExists := fileExists(app.dataDir + "/chain_salt.anchor")
		isNew := !chainSaltExists

		
		log.Printf("[AUTH STATUS] dataDir=%s unlocked=%v chainSalt=%v isNew=%v", app.dataDir, unlocked, chainSaltExists, isNew)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"needs_passkey":  !unlocked,
			"is_new":         isNew,
			"stateless_mode": true,
			"has_chain_salt": chainSaltExists,
		})
	}))

	
	mux.HandleFunc("/auth/unlock", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Passkey string `json:"passkey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		app.mu.Lock()
		defer app.mu.Unlock()

		if app.unlocked {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}

		passkeySecret := []byte(req.Passkey)

		
		log.Printf("[AUTH UNLOCK] Deriving identity from secret (len=%d)", len(passkeySecret))
		unifiedID, err := identity.NewUnifiedIdentity(identity.UnifiedConfig{
			Mode:       identity.ModeStateless,
			DataDir:    app.dataDir,
			UserSecret: passkeySecret,
		})

		
		for i := range passkeySecret {
			passkeySecret[i] = 0
		}

		if err != nil {
			log.Printf("[AUTH UNLOCK] Error deriving identity: %v", err)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}

		log.Printf("[AUTH UNLOCK] Success! Address=%s", unifiedID.Address.Hex())
		app.unifiedID = unifiedID
		
		app.id = &identity.Identity{
			PrivateKey: unifiedID.PrivateKey,
			Address:    unifiedID.Address,
			ForkID:     unifiedID.ForkID,
			ShortID:    unifiedID.ShortID,
		}
		app.unlocked = true

		
		go app.startP2P(*p2pPeer)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"stateless_mode": true,
			"address":        app.id.Address.Hex(),
		})
	}))

	
	mux.HandleFunc("/health", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		defer app.mu.RUnlock()

		resp := map[string]interface{}{
			"status":         "ok",
			"ngac":           true,
			"words":          app.backend.WordCount(),
			"stateless_mode": true,
		}

		if app.id != nil {
			resp["fork_id"] = fmt.Sprintf("0x%x", app.id.ForkID[:8])
			resp["address"] = app.id.Address.Hex()
			resp["unlocked"] = true
		} else {
			resp["unlocked"] = false
		}

		if app.unifiedID != nil {
			resp["hardware_gate"] = string(app.unifiedID.GateType())
		}

		if app.p2pTransport != nil {
			resp["p2p_stats"] = app.p2pTransport.Stats()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	
	mux.HandleFunc("/identity", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		defer app.mu.RUnlock()

		if app.id == nil {
			http.Error(w, "Identity not unlocked", http.StatusUnauthorized)
			return
		}

		resp := map[string]interface{}{
			"address":        app.id.Address.Hex(),
			"fork_id":        fmt.Sprintf("0x%x", app.id.ForkID),
			"short_id":       app.id.ShortID,
			"stateless_mode": true,
		}

		
		if app.unifiedID != nil {
			resp["hardware_gate"] = string(app.unifiedID.GateType())
			resp["is_stateless"] = app.unifiedID.IsStateless()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	
	mux.HandleFunc("/identity/chain-salt", cors(func(w http.ResponseWriter, r *http.Request) {
		
		saltPath := app.dataDir + "/chain_salt.anchor"
		saltData, err := os.ReadFile(saltPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Chain salt not found",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"chain_salt":     fmt.Sprintf("%x", saltData),
			"chain_salt_hex": fmt.Sprintf("0x%x", saltData),
			"warning":        "This is your chain salt. Combined with your secret phrase, it derives your identity. Back this up securely.",
		})
	}))

	
	mux.HandleFunc("/generate", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Prompt    string  `json:"prompt"`
			MaxTokens int     `json:"max_tokens"`
			Temp      float32 `json:"temperature"`
			Broadcast bool    `json:"broadcast"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.MaxTokens == 0 {
			req.MaxTokens = 50
		}
		if req.Temp == 0 {
			req.Temp = 0.7
		}

		response, err := app.backend.GenerateText(req.Prompt, req.MaxTokens, req.Temp, 0.9, 40)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		app.mu.RLock()
		shortID := ""
		if app.id != nil {
			shortID = app.id.ShortID
		}
		app.mu.RUnlock()

		result := map[string]interface{}{
			"response": response,
			"pressure": app.backend.GetPressure(),
			"fork_id":  shortID,
		}

		
		if req.Broadcast {
			app.mu.RLock()
			p2p := app.p2pTransport
			id := app.id
			app.mu.RUnlock()

			if p2p != nil && id != nil {
				hash, err := p2p.BroadcastForkBlock(id.ForkID, []byte(response))
				if err != nil {
					result["broadcast_error"] = err.Error()
				} else {
					result["broadcast_tx"] = hash.Hex()
					fmt.Printf("[ZEAM] Broadcast to L1 P2P: %s\n", hash.Hex())
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	
	mux.HandleFunc("/hash", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Prompt    string   `json:"prompt"`
			Concepts  []string `json:"concepts"`  
			MaxWords  int      `json:"max_words"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.MaxWords == 0 {
			req.MaxWords = 10
		}

		if !app.backend.IsHashEnabled() {
			http.Error(w, "Hash network not enabled", http.StatusServiceUnavailable)
			return
		}

		var response string
		var words []string
		var err error

		if len(req.Concepts) > 0 {
			
			words, err = app.backend.HashForward(req.Concepts)
			if err == nil {
				response = strings.Join(words, " ")
			}
		} else {
			
			response, err = app.backend.HashGenerate(req.Prompt, req.MaxWords)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		app.mu.RLock()
		forkID := ""
		if app.id != nil {
			forkID = fmt.Sprintf("%x", app.id.ForkID)
		}
		app.mu.RUnlock()

		pressure := app.backend.GetPressure()
		l1Peers, relays, blockAge := app.backend.GetNetworkState()

		result := map[string]interface{}{
			"response":     response,
			"words":        words,
			"fork_id":      forkID,
			"hash_network": true,
			"pressure": map[string]interface{}{
				"magnitude": pressure.Magnitude,
				"coherence": pressure.Coherence,
				"tension":   pressure.Tension,
				"density":   pressure.Density,
			},
			"network": map[string]interface{}{
				"l1_peers":  l1Peers,
				"relays":    relays,
				"block_age": blockAge,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	
	mux.HandleFunc("/collapse", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Prompt    string   `json:"prompt"`
			Chain     []string `json:"chain"` 
			Broadcast bool     `json:"broadcast"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if !app.backend.IsCollapseEnabled() {
			http.Error(w, "Collapse compute not enabled - unlock identity first", http.StatusServiceUnavailable)
			return
		}

		var response string
		var hashes []string
		var err error

		if len(req.Chain) > 0 {
			
			var txHashes []common.Hash
			response, txHashes, err = app.backend.CollapseChainGenerate(req.Chain)
			for _, h := range txHashes {
				hashes = append(hashes, h.Hex())
			}
		} else {
			
			var txHash common.Hash
			response, txHash, err = app.backend.CollapseGenerate(req.Prompt)
			hashes = []string{txHash.Hex()}
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		app.mu.RLock()
		forkID := ""
		if app.id != nil {
			forkID = fmt.Sprintf("%x", app.id.ForkID)
		}
		app.mu.RUnlock()

		
		pressure := app.backend.GetPressure()
		l1Peers, relays, blockAge := app.backend.GetNetworkState()

		result := map[string]interface{}{
			"response":   response,
			"tx_hashes":  hashes,
			"fork_id":    forkID,
			"collapse":   true,
			"l1_compute": "L1 nodes perform hash computation",
			"pressure": map[string]interface{}{
				"magnitude": pressure.Magnitude,
				"coherence": pressure.Coherence,
				"tension":   pressure.Tension,
				"density":   pressure.Density,
			},
			"network": map[string]interface{}{
				"l1_peers":  l1Peers,
				"relays":    relays,
				"block_age": blockAge,
			},
		}

		
		if semanticPath := app.backend.GetSemanticPath(); semanticPath != nil {
			result["semantic_path"] = semanticPath
		}

		
		if req.Broadcast {
			app.mu.RLock()
			p2p := app.p2pTransport
			id := app.id
			app.mu.RUnlock()

			if p2p != nil && id != nil {
				
				queryHash := sha256.Sum256([]byte(req.Prompt))
				hash, err := p2p.BroadcastCollapseInput(queryHash, []byte(req.Prompt))
				if err != nil {
					result["broadcast_error"] = err.Error()
				} else {
					result["broadcast_tx"] = hash.Hex()
					fmt.Printf("[ZEAM] Collapse broadcast to L1: %s\n", hash.Hex())
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	
	mux.HandleFunc("/broadcast", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		app.mu.RLock()
		p2p := app.p2pTransport
		id := app.id
		app.mu.RUnlock()

		if p2p == nil || id == nil {
			http.Error(w, "Identity not unlocked", http.StatusUnauthorized)
			return
		}

		var req struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var hash string
		var txErr error

		switch req.Type {
		case "query":
			h, err := p2p.BroadcastLLMQuery([]byte(req.Message))
			hash, txErr = h.Hex(), err
		default:
			h, err := p2p.BroadcastForkBlock(id.ForkID, []byte(req.Message))
			hash, txErr = h.Hex(), err
		}

		if txErr != nil {
			http.Error(w, txErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "broadcast",
			"tx":     hash,
		})
	}))

	
	mux.HandleFunc("/p2p", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		p2p := app.p2pTransport
		app.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		if p2p != nil {
			json.NewEncoder(w).Encode(p2p.Stats())
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "not_started"})
		}
	}))

	
	mux.HandleFunc("/pressure", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(app.backend.GetPressure())
	}))

	
	mux.HandleFunc("/flow-stream", cors(func(w http.ResponseWriter, r *http.Request) {
		
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				
				pressure := app.backend.GetPressure()
				l1Peers, relays, blockAge := app.backend.GetNetworkState()
				dynamics := app.backend.GetFlowDynamics()

				data := map[string]interface{}{
					"type": "flow_update",
					"pressure": map[string]float64{
						"magnitude": pressure.Magnitude,
						"coherence": pressure.Coherence,
						"tension":   pressure.Tension,
						"density":   pressure.Density,
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
				}

				jsonData, _ := json.Marshal(data)
				fmt.Fprintf(w, "data: %s\n\n", jsonData)
				flusher.Flush()
			}
		}
	}))

	
	if *uiDir != "" {
		fmt.Printf("[ZEAM] Serving UI from %s\n", *uiDir)
		mux.Handle("/", http.FileServer(http.Dir(*uiDir)))
	}

	
	addr := ":" + *port
	fmt.Printf("[ZEAM] HTTP API on %s\n", addr)
	fmt.Println()
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /auth/status         - Check if passkey needed")
	fmt.Println("  POST /auth/unlock         - Submit passkey")
	fmt.Println("  GET  /health              - Status + P2P stats")
	fmt.Println("  GET  /identity            - Fork identity")
	fmt.Println("  GET  /identity/chain-salt - Chain salt backup (stateless mode)")
	fmt.Println("  POST /generate            - NGAC generation")
	fmt.Println("  POST /hash                - Hash-native tensor generation")
	fmt.Println("  POST /collapse            - L1 collapse compute")
	fmt.Println("  POST /broadcast           - Broadcast to L1 mempool")
	fmt.Println("  GET  /p2p                 - P2P connection status")
	fmt.Println("  GET  /pressure            - Pressure state")
	fmt.Println()

	
	if app.unlocked {
		go app.startP2P(*p2pPeer)
	}

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatal(err)
		}
	}()

	fmt.Println("[ZEAM] Ready. Ctrl+C to exit.")
	if !app.unlocked {
		fmt.Println("[ZEAM] Waiting for passkey via UI...")
	}

	
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n[ZEAM] Shutting down...")
	app.mu.RLock()
	if app.p2pTransport != nil {
		app.p2pTransport.Stop()
	}
	if app.hybridTransport != nil {
		app.hybridTransport.Stop()
	}
	app.mu.RUnlock()
}

func (app *App) startP2P(bootstrapPeer string) {
	app.mu.RLock()
	id := app.id
	useRelay := app.useRelay
	app.mu.RUnlock()

	if id == nil {
		return
	}

	
	if useRelay {
		app.startHybridP2P(bootstrapPeer)
		return
	}

	
	fmt.Println("[ZEAM] Starting L1 P2P transport (Sepolia) - direct mode...")

	config := node.SepoliaConfig(id.PrivateKey)
	config.ListenAddr = app.p2pListen

	p2pTransport, err := node.NewP2PTransport(id.PrivateKey, config)
	if err != nil {
		fmt.Printf("[ZEAM] Failed to create P2P transport: %v\n", err)
		return
	}

	
	p2pTransport.OnZEAMBlock = func(forkID [32]byte, data []byte) {
		fmt.Printf("[ZEAM] Received FORK_BLOCK from %x... (%d bytes)\n", forkID[:8], len(data))
	}

	p2pTransport.OnZEAMGenesis = func(forkID [32]byte, data []byte) {
		fmt.Printf("[ZEAM] Received FORK_GENESIS from %x... (%d bytes)\n", forkID[:8], len(data))
	}

	p2pTransport.OnZEAMCrossMsg = func(from, to [32]byte, encrypted []byte) {
		if to == id.ForkID {
			fmt.Printf("[ZEAM] Received encrypted message from %x...\n", from[:8])
		}
	}

	p2pTransport.OnLLMQuery = func(data []byte) []byte {
		prompt := string(data)
		fmt.Printf("[ZEAM] Received NGAC query: %q\n", prompt)

		response, err := app.backend.GenerateText(prompt, 50, 0.7, 0.9, 40)
		if err != nil {
			fmt.Printf("[ZEAM] NGAC error: %v\n", err)
			return nil
		}

		fmt.Printf("[ZEAM] NGAC response: %q\n", response)
		return []byte(response)
	}

	p2pTransport.OnLLMResponse = func(data []byte) {
		fmt.Printf("[ZEAM] Received NGAC response: %q\n", string(data))
	}

	
	p2pTransport.OnCollapseInput = func(queryHash [32]byte, input []byte) {
		fmt.Printf("[ZEAM] Received collapse input: %x... (%d bytes)\n", queryHash[:8], len(input))

		
		if app.backend.IsCollapseEnabled() {
			response, txHash, err := app.backend.CollapseGenerate(string(input))
			if err == nil {
				
				semantics := []byte(response)
				p2pTransport.BroadcastCollapseOutput(queryHash, txHash, semantics)
				fmt.Printf("[ZEAM] Collapse computed: %s -> %s\n", txHash.Hex()[:16], response)
			}
		}
	}

	p2pTransport.OnCollapseOutput = func(queryHash [32]byte, txHash common.Hash, semantics []byte) {
		fmt.Printf("[ZEAM] Received collapse output: query=%x... hash=%s result=%q\n",
			queryHash[:8], txHash.Hex()[:16], string(semantics))
	}

	if err := p2pTransport.Start(); err != nil {
		fmt.Printf("[ZEAM] Failed to start P2P transport: %v\n", err)
		return
	}

	if bootstrapPeer != "" {
		if err := p2pTransport.AddPeer(bootstrapPeer); err != nil {
			fmt.Printf("[ZEAM] Warning: failed to add peer: %v\n", err)
		} else {
			fmt.Printf("[ZEAM] Added bootstrap peer\n")
		}
	}

	app.mu.Lock()
	app.p2pTransport = p2pTransport
	app.mu.Unlock()

	
	sepoliaChainID := big.NewInt(11155111)
	app.backend.EnableCollapseCompute(sepoliaChainID, id.Address)

	fmt.Printf("[ZEAM] P2P transport ready on %s\n", app.p2pListen)
	fmt.Printf("[ZEAM] Address: %s\n", id.Address.Hex())
	fmt.Printf("[ZEAM] Fork ID: 0x%x\n", id.ForkID[:8])
	fmt.Printf("[ZEAM] Collapse compute: ENABLED (L1 as neural network)\n")

	
	go func() {
		fmt.Println("[ZEAM] Waiting for peers to sync fork...")
		if err := p2pTransport.WaitForPeers(1, 30*time.Second); err != nil {
			fmt.Printf("[ZEAM] No peers found, starting fresh fork\n")
			return
		}

		blocks, err := p2pTransport.RequestForkSync(id.ForkID, 0, 10*time.Second)
		if err != nil {
			fmt.Printf("[ZEAM] Fork sync error: %v\n", err)
			return
		}

		if len(blocks) > 0 {
			fmt.Printf("[ZEAM] Recovered %d blocks from mesh for fork %x...\n", len(blocks), id.ForkID[:8])
		} else {
			fmt.Printf("[ZEAM] No existing blocks found, this is a fresh fork\n")
		}
	}()
}


func (app *App) startHybridP2P(bootstrapPeer string) {
	app.mu.RLock()
	id := app.id
	app.mu.RUnlock()

	if id == nil {
		return
	}

	fmt.Println("[ZEAM] Starting hybrid P2P transport (libp2p + devp2p)...")
	fmt.Println("[ZEAM] Using IPFS relay network for NAT traversal")

	hybridTransport, err := node.CreateSepoliaHybridTransport(id.PrivateKey, app.p2pListen, true)
	if err != nil {
		fmt.Printf("[ZEAM] Failed to create hybrid transport: %v\n", err)
		return
	}

	
	hybridTransport.OnZEAMMessage(func(tx *types.Transaction, data []byte) {
		fmt.Printf("[ZEAM] Received ZEAM message: %d bytes\n", len(data))
	})

	hybridTransport.OnPeerConnect(func(peerID string) {
		fmt.Printf("[ZEAM] L1 peer connected: %s\n", peerID)
	})

	hybridTransport.OnPeerDrop(func(peerID string) {
		fmt.Printf("[ZEAM] L1 peer disconnected: %s\n", peerID)
	})

	if err := hybridTransport.Start(); err != nil {
		fmt.Printf("[ZEAM] Failed to start hybrid transport: %v\n", err)
		return
	}

	app.mu.Lock()
	app.hybridTransport = hybridTransport
	app.mu.Unlock()

	
	liveCompute := node.NewLiveCompute(hybridTransport)
	liveCompute.SetTimeout(5 * time.Second)
	app.backend.SetLiveDispatcher(&liveDispatcherAdapter{lc: liveCompute})
	fmt.Println("[ZEAM] Live dispatcher enabled - NGAC will use L1 network for computation")

	
	unifiedCognition := node.NewUnifiedCognition(hybridTransport)
	if err := unifiedCognition.Start(); err != nil {
		fmt.Printf("[ZEAM] Warning: failed to start unified cognition: %v\n", err)
	} else {
		fmt.Println("[ZEAM] Unified cognition started - Flow + Broadcasts + User Input → One River")
	}

	
	unifiedAdapter := &unifiedCognitionAdapter{
		unified:     unifiedCognition,
		flowCompute: node.NewUnifiedFlowCompute(unifiedCognition),
	}
	app.backend.SetFlowCognition(unifiedAdapter, unifiedAdapter)
	fmt.Println("[ZEAM] V3 unified flow generation ENABLED (all sources feed one cognitive state)")

	
	sepoliaChainID := big.NewInt(11155111)
	app.backend.EnableCollapseCompute(sepoliaChainID, id.Address)

	fmt.Printf("[ZEAM] Hybrid P2P transport ready on %s\n", app.p2pListen)
	fmt.Printf("[ZEAM] Address: %s\n", id.Address.Hex())
	fmt.Printf("[ZEAM] LibP2P Peer ID: %s\n", hybridTransport.LibP2PPeerID()[:16])
	fmt.Printf("[ZEAM] Collapse compute: ENABLED (L1 as neural network)\n")
	fmt.Printf("[ZEAM] Flow cognition: ENABLED (continuous L1 mempool thinking)\n")

	
	go func() {
		ticker := time.NewTicker(1 * time.Second) 
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				relays, _ := hybridTransport.Stats()
				l1Peers := hybridTransport.PeerCount()
				relayAddrs := hybridTransport.RelayAddrs()

				
				app.mu.RLock()
				backend := app.backend
				app.mu.RUnlock()

				if backend != nil {
					
					
					pseudoBlockHash := make([]byte, 8)
					pseudoBlockHash[0] = byte(l1Peers)
					pseudoBlockHash[1] = byte(relays)
					pseudoBlockHash[2] = byte(time.Now().Unix())
					pseudoBlockHash[3] = byte(len(relayAddrs))

					
					backend.UpdateNetworkState(l1Peers, relays, pseudoBlockHash)
				}

				
				if relays > 0 || l1Peers > 0 {
					fmt.Printf("[ZEAM] Live: %d L1 peers, %d relays (collapse using network state)\n", l1Peers, len(relayAddrs))
				}
			}
		}
	}()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readPasskey(prompt string) []byte {
	fmt.Print(prompt)
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return nil
	}
	return password
}

func createNewPasskey() []byte {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Create passkey: ")
		pass1, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil || len(pass1) == 0 {
			fmt.Println("Passkey cannot be empty")
			continue
		}

		if len(pass1) < 8 {
			fmt.Println("Passkey must be at least 8 characters")
			continue
		}

		fmt.Print("Confirm passkey: ")
		pass2, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			continue
		}

		if string(pass1) != string(pass2) {
			fmt.Println("Passkeys do not match. Try again.")
			continue
		}

		fmt.Println()
		fmt.Println("=== RECOVERY PHRASE (SAVE THIS!) ===")
		phrase, secret, err := identity.GenerateRecoveryPhrase()
		if err != nil {
			log.Fatalf("Failed to generate recovery phrase: %v", err)
		}
		fmt.Println(phrase)
		fmt.Println("=====================================")
		fmt.Println("This phrase can recover your identity if you forget your passkey.")
		fmt.Println()

		fmt.Print("Press Enter to continue...")
		reader.ReadString('\n')

		combined := append(pass1, secret...)
		return combined
	}
}


func hashSecret(passkey string) []byte {
	h := sha256.Sum256([]byte(passkey))
	return h[:]
}
