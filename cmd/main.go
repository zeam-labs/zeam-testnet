package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"zeam/arb"
	"zeam/content"
	"zeam/arb/routing"
	"zeam/identity"
	"zeam/rewards"
	"zeam/synthesis"
	"zeam/ngac"
	"zeam/node"
	"zeam/pong"
	"zeam/quantum"
	"zeam/update"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/term"
)

var embeddedUI embed.FS

var embeddedOEWNGz []byte

type App struct {
	mu              sync.RWMutex
	id              *identity.Identity
	unifiedID       *identity.UnifiedIdentity
	backend         *synthesis.NGACBackend
	p2pTransport    *node.P2PTransport
	hybridTransport *node.HybridTransport
	multiChain      *node.MultiChainTransport
	arbDetector     *arb.MultiHopDetector
	flowOrchestrator *arb.FlowOrchestrator
	pongEngine      *pong.AdversaryEngine
	dataDir         string
	oewnPath        string
	p2pListen       string
	useRelay        bool
	useTestnet      bool

	contentStore      *content.Store
	contentDAG        *content.DAGBuilder
	contentProtocol   *content.ContentProtocol
	contentUIRoot     content.ContentID
	contentPinnedRoot content.ContentID

	rewardsConfig    *rewards.Config
	epochManager     *rewards.EpochManager
	challengeManager *rewards.ChallengeManager
	storageTracker   *rewards.StorageTracker
	poolClient       *rewards.PoolClient

	updateChecker *update.Checker
	updateConfig  *update.Config

	unlocked        bool
	statelessMode   bool
}

type liveDispatcherAdapter struct {
	lc *node.LiveCompute
}

func (a *liveDispatcherAdapter) Dispatch(ctx context.Context, payload []byte) (*synthesis.DispatchResult, error) {
	result, err := a.lc.Dispatch(ctx, payload)
	if err != nil {
		return nil, err
	}
	return &synthesis.DispatchResult{
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
	cognition *ngac.ActiveCognition
}

type unifiedCognitionAdapter struct {
	unified     *ngac.UnifiedCognition
	flowCompute *ngac.UnifiedFlowCompute
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

func (f *flowCognitionAdapter) Inject(inj synthesis.FlowInjection) {
	if f.cognition == nil {
		return
	}
	f.cognition.Inject(ngac.Injection{
		Type:     ngac.InjectionType(inj.Type),
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

func (f *flowCognitionAdapter) State() synthesis.FlowCognitiveState {
	if f.cognition == nil {
		return synthesis.FlowCognitiveState{}
	}
	state := f.cognition.State()
	return synthesis.FlowCognitiveState{
		ActiveConcepts: state.ActiveConcepts,
		ConceptWeights: state.ConceptWeights,
		ReadyToRespond: state.ReadyToRespond,
	}
}

func (f *flowCognitionAdapter) Dynamics() synthesis.FlowDynamicsState {
	if f.cognition == nil {
		return synthesis.FlowDynamicsState{Coherence: 0.5}
	}
	d := f.cognition.Dynamics()
	return synthesis.FlowDynamicsState{
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

func (u *unifiedCognitionAdapter) Inject(inj synthesis.FlowInjection) {
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

func (u *unifiedCognitionAdapter) State() synthesis.FlowCognitiveState {
	if u.unified == nil {
		return synthesis.FlowCognitiveState{}
	}
	concepts := u.unified.ActiveConcepts(10)
	weights := make(map[string]float64)
	for _, c := range concepts {
		weights[c] = u.unified.ConceptWeight(c)
	}
	dynamics := u.unified.Dynamics()
	return synthesis.FlowCognitiveState{
		ActiveConcepts: concepts,
		ConceptWeights: weights,
		ReadyToRespond: dynamics.ReadyToAct,
	}
}

func (u *unifiedCognitionAdapter) Dynamics() synthesis.FlowDynamicsState {
	if u.unified == nil {
		return synthesis.FlowDynamicsState{Coherence: 0.5}
	}
	d := u.unified.Dynamics()
	return synthesis.FlowDynamicsState{
		Pressure:  d.FlowPressure,
		Tension:   d.FlowTension,
		Rhythm:    d.FlowRhythm,
		Coherence: d.FlowCoherence,
	}
}

type simpleFlowAdapter struct {
	compute *node.FlowCompute
}

func (s *simpleFlowAdapter) NextBytes(n int) []byte {
	if s.compute == nil {
		return make([]byte, n)
	}
	return s.compute.NextBytes(n)
}

func (s *simpleFlowAdapter) NextUint64() uint64 {
	if s.compute == nil {
		return 0
	}
	return s.compute.NextUint64()
}

func (s *simpleFlowAdapter) SelectIndex(n int) int {
	if s.compute == nil || n <= 0 {
		return 0
	}
	return s.compute.SelectIndex(n)
}

func (s *simpleFlowAdapter) Inject(inj synthesis.FlowInjection) {}
func (s *simpleFlowAdapter) Query(content string, concepts []string) {}
func (s *simpleFlowAdapter) Steer(concepts []string, strength float64) {}
func (s *simpleFlowAdapter) Focus(concepts []string) {}
func (s *simpleFlowAdapter) State() synthesis.FlowCognitiveState {
	return synthesis.FlowCognitiveState{}
}
func (s *simpleFlowAdapter) Dynamics() synthesis.FlowDynamicsState {
	if s.compute == nil {
		return synthesis.FlowDynamicsState{
			Pressure: 0.3, Tension: 0.3, Rhythm: 0.3, Coherence: 0.3,
		}
	}

	stats := s.compute.Stats()

	pressure := 0.3 + math.Min(0.7, stats.FlowRate/500.0*0.7)

	tension := 0.3 + math.Min(0.7, stats.FlowRate/200.0*0.7)

	age := time.Since(stats.LastHash).Seconds()
	rhythm := math.Max(0.3, 1.0-age/5.0*0.7)

	coherence := 0.5 + 0.3*math.Sin(float64(stats.HashCount)/100.0)

	return synthesis.FlowDynamicsState{
		Pressure:  pressure,
		Tension:   tension,
		Rhythm:    rhythm,
		Coherence: coherence,
	}
}

type pongFlowAdapter struct {
	flow *node.FlowCompute
}

func (p *pongFlowAdapter) NextBytes(n int) []byte {
	if p.flow == nil {
		return make([]byte, n)
	}
	return p.flow.NextBytes(n)
}

func (p *pongFlowAdapter) Stats() (hashCount uint64, flowRate float64) {
	if p.flow == nil {
		return 0, 0
	}
	stats := p.flow.Stats()
	return stats.HashCount, stats.FlowRate
}

func main() {

	defer func() {
		if r := recover(); r != nil {
			crashLog := fmt.Sprintf("ZEAM CRASH at %s\nPanic: %v\n", time.Now().Format(time.RFC3339), r)

			for _, path := range []string{"./zeam_crash.log", os.TempDir() + "/zeam_crash.log"} {
				if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); err == nil {
					f.WriteString(crashLog)
					f.Close()
				}
			}
			fmt.Fprintf(os.Stderr, "\n\n%s\n", crashLog)
			panic(r)
		}
	}()

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
	useTestnet := flag.Bool("testnet", false, "Use testnet chains (Sepolia, Base Sepolia, OP Sepolia)")
	noP2P := flag.Bool("no-p2p", false, "Disable P2P networking (local mode only)")

	ipcSocket := flag.String("ipc", "", "Unix socket path for IPC (enables native shell mode)")

	testPasskey := flag.String("test-passkey", "", "Passkey for testing (skips UI unlock)")

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
		useTestnet:    *useTestnet,
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

	resolvedOEWN := *oewnPath
	if _, err := os.Stat(resolvedOEWN); err != nil {

		fmt.Println("[ZEAM] Extracting embedded OEWN data...")
		gr, err := gzip.NewReader(bytes.NewReader(embeddedOEWNGz))
		if err != nil {
			log.Fatalf("[ZEAM] Failed to decompress embedded OEWN: %v", err)
		}
		tmpDir := filepath.Join(resolvedDataDir, "cache")
		os.MkdirAll(tmpDir, 0700)
		tmpFile := filepath.Join(tmpDir, "oewn.json")
		f, err := os.Create(tmpFile)
		if err != nil {
			log.Fatalf("[ZEAM] Failed to create temp OEWN file: %v", err)
		}
		if _, err := io.Copy(f, gr); err != nil {
			f.Close()
			log.Fatalf("[ZEAM] Failed to write OEWN data: %v", err)
		}
		f.Close()
		gr.Close()
		resolvedOEWN = tmpFile
		fmt.Printf("[ZEAM] Extracted OEWN to %s\n", tmpFile)
	}
	fmt.Printf("[ZEAM] Loading NGAC with OEWN from %s...\n", resolvedOEWN)
	backend, err := synthesis.NewNGACBackend(resolvedOEWN)
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

	backend.RegisterWithQuantumService()
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

		if !*noP2P {
			go app.startP2P(*p2pPeer)
		} else {
			fmt.Println("[ZEAM] P2P disabled (--no-p2p flag)")
		}

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

		resp["collapse_enabled"] = app.backend.IsCollapseEnabled()

		if app.multiChain != nil {
			l1Chains := app.multiChain.GetL1Chains()
			resp["multichain_ready"] = true
			resp["l1_chains"] = len(l1Chains)
			resp["all_chains"] = len(app.multiChain.GetEnabledChains())
		} else {
			resp["multichain_ready"] = false
			resp["l1_chains"] = 0
			resp["all_chains"] = 0
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
				"hadamard": pressure.Hadamard,
				"pauliX":   pressure.PauliX,
				"pauliZ":   pressure.PauliZ,
				"phase":    pressure.Phase,
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
				"hadamard": pressure.Hadamard,
				"pauliX":   pressure.PauliX,
				"pauliZ":   pressure.PauliZ,
				"phase":    pressure.Phase,
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
		qs := quantum.GetService()
		json.NewEncoder(w).Encode(qs.GetPressure())
	}))

	mux.HandleFunc("/debug/quantum", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		qs := quantum.GetService()
		stats := qs.Stats()

		stats["current_pressure"] = qs.GetPressure()
		json.NewEncoder(w).Encode(stats)
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

				qs := quantum.GetService()
				pressure := qs.GetPressure()
				l1Peers, relays, blockAge := app.backend.GetNetworkState()
				dynamics := app.backend.GetFlowDynamics()

				data := map[string]interface{}{
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
				}

				jsonData, _ := json.Marshal(data)
				fmt.Fprintf(w, "data: %s\n\n", jsonData)
				flusher.Flush()
			}
		}
	}))

	mux.HandleFunc("/pong/state", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		engine := app.pongEngine
		app.mu.RUnlock()

		if engine == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"running": false,
				"message": "Pong not started. POST /pong/start to begin.",
			})
			return
		}

		state := engine.State()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		})
	}))

	mux.HandleFunc("/pong/start", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		app.mu.Lock()
		defer app.mu.Unlock()

		if app.pongEngine != nil {
			app.pongEngine.Stop()
		}

		computeHash := func(payload []byte) (common.Hash, error) {
			if app.multiChain == nil {
				return common.Hash{}, fmt.Errorf("network not connected")
			}

			flowEntropy := app.multiChain.GetUnifiedEntropy()
			if len(flowEntropy) == 0 {
				return common.Hash{}, fmt.Errorf("no flow entropy available - waiting for network")
			}

			combined := make([]byte, 32)
			copy(combined, flowEntropy)
			for i := 0; i < len(payload) && i < 32; i++ {
				combined[i] ^= payload[i]
			}

			ts := uint64(time.Now().UnixNano())
			for i := 0; i < 8; i++ {
				combined[24+i] ^= byte(ts >> (i * 8))
			}
			return common.BytesToHash(combined), nil
		}

		flowCompute := app.multiChain.GetFlowCompute()
		flowProvider := &pongFlowAdapter{flow: flowCompute}

		app.pongEngine = pong.NewAdversaryEngine(computeHash, flowProvider)
		app.pongEngine.SetTickRate(50 * time.Millisecond)
		app.pongEngine.SetAdversaryDifficulty(0.6)

		app.pongEngine.OnScore = func(player, s1, s2 uint8) {
			fmt.Printf("[Pong] Player %d scores! %d-%d\n", player, s1, s2)
		}
		app.pongEngine.OnGameOver = func(winner uint8) {
			fmt.Printf("[Pong] Game over! Player %d wins!\n", winner)
		}

		app.pongEngine.Start()
		fmt.Println("[Pong] Game started - physics + AI computed by unified network flow")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "started",
			"message": "Pong started. Every tick computed by distributed network across all chains.",
		})
	}))

	mux.HandleFunc("/pong/input", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Player int    `json:"player"`
			Input  string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		app.mu.RLock()
		engine := app.pongEngine
		app.mu.RUnlock()

		if engine == nil {
			http.Error(w, "Pong not started", http.StatusBadRequest)
			return
		}

		var input pong.Input
		switch req.Input {
		case "up":
			input = pong.InputUp
		case "down":
			input = pong.InputDown
		default:
			input = pong.InputNone
		}

		engine.SetInput(uint8(req.Player), input)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	mux.HandleFunc("/pong/stop", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		app.mu.Lock()
		if app.pongEngine != nil {
			app.pongEngine.Stop()
			app.pongEngine = nil
		}
		app.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "stopped"})
	}))

	mux.HandleFunc("/pong/reset", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		app.mu.RLock()
		engine := app.pongEngine
		app.mu.RUnlock()

		if engine == nil {
			http.Error(w, "Pong not started", http.StatusBadRequest)
			return
		}

		engine.Reset()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "reset"})
	}))

	mux.HandleFunc("/arb/stats", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		detector := app.arbDetector
		app.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")

		if detector == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Arb detector not initialized",
			})
			return
		}

		stats := detector.Stats()
		neuralStats := detector.NeuralStats()

		routesExplored := 0
		routesFoundNeural := 0
		pressureCollapses := 0
		totalPressure := 0.0

		if nf, ok := neuralStats["neural_finder"].(map[string]interface{}); ok {
			if v, ok := nf["routes_explored"].(uint64); ok {
				routesExplored = int(v)
			}
			if v, ok := nf["routes_found"].(uint64); ok {
				routesFoundNeural = int(v)
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

		json.NewEncoder(w).Encode(map[string]interface{}{
			"pools_discovered":   stats.PoolsDiscovered,
			"graph_nodes":        stats.GraphNodes,
			"graph_edges":        stats.GraphEdges,
			"routes_found":       stats.RoutesFound,
			"routes_filtered":    stats.RoutesFiltered,
			"best_profit_bps":    stats.BestProfitBPS,
			"routes_explored":    routesExplored,
			"routes_found_neural": routesFoundNeural,
			"pressure_collapses": pressureCollapses,
			"total_pressure":     totalPressure,
			"txs_processed":      stats.TxsProcessed,
			"last_scan":          stats.LastRouteScan.Unix(),
			"scan_duration_ms":   stats.ScanDuration.Milliseconds(),
		})
	}))

	mux.HandleFunc("/arb/routes", cors(func(w http.ResponseWriter, r *http.Request) {
		app.mu.RLock()
		detector := app.arbDetector
		app.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")

		if detector == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"routes": []interface{}{},
			})
			return
		}

		routes := detector.GetRecentRoutes(20)

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
				"id":          idHex,
				"profit_bps":  route.ProfitBPS,
				"start_chain": route.StartChain,
				"start_token": route.StartToken.Hex(),
				"hops":        hops,
				"is_cross_chain": route.IsCrossChain,
			})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"routes": routeList,
		})
	}))

	app.contentStore = content.NewStore("")
	app.contentDAG = content.NewDAGBuilder(app.contentStore, content.DefaultChunkSize)

	if uiData, err := embeddedUI.ReadFile("ui/index.html"); err == nil && len(uiData) > 0 {
		uiRoot, err := app.contentDAG.Add(uiData)
		if err == nil {
			app.contentUIRoot = uiRoot
			app.contentPinnedRoot = uiRoot
			fmt.Printf("[ZEAM] Content layer: UI seeded as %s (%d bytes)\n", uiRoot.Hex()[:16], len(uiData))
		}
	}

	go func() {

		time.Sleep(3 * time.Second)
		if app.multiChain != nil {
			h := app.multiChain.SharedHost()
			if h != nil {
				providers := content.NewProviderRegistry(time.Hour)
				cp := content.NewContentProtocol(h, app.contentStore, app.contentDAG, providers)
				topic, sub, err := app.multiChain.JoinContentTopic(content.ContentTopicName)
				if err == nil {
					cp.Start(topic, sub)
					if app.contentUIRoot != (content.ContentID{}) {
						cp.Announce(app.contentUIRoot, uint64(app.contentStore.TotalBytes()))
					}
					app.contentProtocol = cp
					fmt.Printf("[ZEAM] Content protocol active on P2P network\n")
				} else {

					cp.Start(nil, nil)
					app.contentProtocol = cp
					fmt.Printf("[ZEAM] Content protocol active (local only, no gossip: %v)\n", err)
				}
			}
		}
	}()

	mux.HandleFunc("/content/", cors(func(w http.ResponseWriter, r *http.Request) {
		hashHex := strings.TrimPrefix(r.URL.Path, "/content/")
		hashHex = strings.TrimSuffix(hashHex, "/")

		if hashHex == "" {

			json.NewEncoder(w).Encode(map[string]interface{}{
				"chunks":      app.contentStore.Size(),
				"total_bytes": app.contentStore.TotalBytes(),
				"ui_root":     app.contentUIRoot.Hex(),
				"pinned_root": app.contentPinnedRoot.Hex(),
			})
			return
		}

		id, err := content.ParseContentID(hashHex)
		if err != nil {
			http.Error(w, "invalid content hash", http.StatusBadRequest)
			return
		}

		data, err := app.contentDAG.Get(id)
		if err != nil {

			if app.contentProtocol != nil {
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				data, err = app.contentProtocol.FetchContent(ctx, id)
			}
			if err != nil {
				http.Error(w, "content not found", http.StatusNotFound)
				return
			}
		}

		if len(data) > 0 && data[0] == '<' {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else if len(data) > 4 && string(data[:4]) == "%PDF" {
			w.Header().Set("Content-Type", "application/pdf")
		} else if len(data) > 2 && data[0] == '{' {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("X-Content-ID", id.Hex())
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(data)
	}))

	mux.HandleFunc("/content/publish", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
		if err != nil || len(body) == 0 {
			http.Error(w, "empty or invalid body", http.StatusBadRequest)
			return
		}

		root, err := app.contentDAG.Add(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if app.contentProtocol != nil {
			app.contentProtocol.Announce(root, uint64(len(body)))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"root": root.Hex(),
			"size": len(body),
			"url":  "/content/" + root.Hex(),
		})
	}))

	mux.HandleFunc("/content/pin", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Root string `json:"root"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		id, err := content.ParseContentID(req.Root)
		if err != nil {
			http.Error(w, "invalid content hash", http.StatusBadRequest)
			return
		}
		if !app.contentStore.Has(id) {

			if _, err := app.contentDAG.Manifest(id); err != nil {
				http.Error(w, "content not found in store", http.StatusNotFound)
				return
			}
		}

		app.contentPinnedRoot = id
		fmt.Printf("[ZEAM] Pinned root updated to %s\n", id.Hex()[:16])

		json.NewEncoder(w).Encode(map[string]interface{}{
			"pinned": id.Hex(),
		})
	}))

	websitePublisher := content.NewWebsitePublisher(app.contentDAG, app.contentStore)

	mux.HandleFunc("/content/publish-site", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 100*1024*1024))
		if err != nil || len(body) == 0 {
			http.Error(w, "empty or invalid body", http.StatusBadRequest)
			return
		}

		manifestID, manifest, err := websitePublisher.PublishTarGz(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if app.contentProtocol != nil {
			app.contentProtocol.Announce(manifestID, manifest.TotalSize)
		}

		fmt.Printf("[ZEAM] Published website: %d files, %d bytes, manifest=%s\n",
			len(manifest.Files), manifest.TotalSize, manifestID.Hex()[:16])

		json.NewEncoder(w).Encode(map[string]interface{}{
			"manifest_id": manifestID.Hex(),
			"files":       len(manifest.Files),
			"total_size":  manifest.TotalSize,
			"pin_url":     "/content/pin",
			"serve_url":   "/site/" + manifestID.Hex() + "/",
		})
	}))

	mux.HandleFunc("/site/", cors(func(w http.ResponseWriter, r *http.Request) {

		path := strings.TrimPrefix(r.URL.Path, "/site/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "missing manifest ID", http.StatusBadRequest)
			return
		}

		manifestID, err := content.ParseContentID(parts[0])
		if err != nil {
			http.Error(w, "invalid manifest ID", http.StatusBadRequest)
			return
		}

		filePath := ""
		if len(parts) > 1 {
			filePath = parts[1]
		}

		data, contentType, err := websitePublisher.GetFile(manifestID, filePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Manifest-ID", manifestID.Hex())
		w.Write(data)
	}))

	app.rewardsConfig = rewards.DefaultConfig()
	app.epochManager = rewards.NewEpochManager(nil, app.contentStore, app.rewardsConfig)
	fmt.Println("[ZEAM] Rewards epoch manager initialized")

	app.epochManager.OnEpochEnd = func(snapshot *rewards.EpochSnapshot) {
		fmt.Printf("[ZEAM] Epoch %d finalized: %d contributors, %s wei\n",
			snapshot.EpochNumber, len(snapshot.Contributors), snapshot.TotalRevenue)

		if app.poolClient != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			tx, err := app.poolClient.FinalizeEpoch(ctx, snapshot.MerkleRoot, snapshot.TotalRevenue)
			if err != nil {
				fmt.Printf("[ZEAM] Failed to submit epoch to chain: %v\n", err)
			} else {
				fmt.Printf("[ZEAM] Epoch %d submitted on-chain: %s\n", snapshot.EpochNumber, tx.Hash().Hex()[:18])
			}
		}
	}

	mux.HandleFunc("/rewards/stats", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		stats := map[string]interface{}{
			"current_epoch":        app.epochManager.CurrentEpoch(),
			"accumulated_revenue":  app.epochManager.GetAccumulatedRevenue().String(),
			"contributor_count":    app.epochManager.ContributorCount(),
			"epoch_duration":       app.rewardsConfig.EpochDuration.String(),
			"optimal_bytes_min":    app.rewardsConfig.OptimalBytesMin,
			"optimal_bytes_max":    app.rewardsConfig.OptimalBytesMax,
			"max_network_share":    app.rewardsConfig.MaxNetworkShare,
		}

		if app.storageTracker != nil {
			trackerStats := app.storageTracker.GetStats()
			for k, v := range trackerStats {
				stats[k] = v
			}
		}

		if app.poolClient != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			if pending, err := app.poolClient.GetPendingRevenue(ctx); err == nil {
				stats["pool_pending_revenue"] = pending.String()
			}
			if epoch, err := app.poolClient.GetCurrentEpoch(ctx); err == nil {
				stats["pool_current_epoch"] = epoch
			}
		}

		json.NewEncoder(w).Encode(stats)
	}))

	mux.HandleFunc("/rewards/history", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		history := app.epochManager.GetHistory()

		epochs := make([]map[string]interface{}, 0, len(history))
		for _, ep := range history {
			epochs = append(epochs, map[string]interface{}{
				"epoch":          ep.EpochNumber,
				"start_time":     ep.StartTime.Unix(),
				"end_time":       ep.EndTime.Unix(),
				"total_revenue":  ep.TotalRevenue.String(),
				"contributors":   len(ep.Contributors),
				"merkle_root":    fmt.Sprintf("%x", ep.MerkleRoot[:8]),
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"epochs": epochs})
	}))

	app.updateConfig = update.DefaultConfig()
	app.updateConfig.DataDir = app.dataDir
	app.updateConfig.CurrentVersion = "0.1.0"

	mux.HandleFunc("/update/status", cors(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if app.updateChecker == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"enabled": false,
				"message": "Update checker not configured (no registry address)",
			})
			return
		}

		status := app.updateChecker.Status()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":           true,
			"current_version":   status.CurrentVersion,
			"latest_version":    status.LatestVersion,
			"update_available":  status.UpdateAvailable,
			"state":             status.State.String(),
			"last_checked":      status.LastChecked.Unix(),
			"download_progress": status.DownloadProgress,
		})
	}))

	mux.HandleFunc("/update/check", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}

		if app.updateChecker == nil {
			http.Error(w, "Update checker not configured", http.StatusServiceUnavailable)
			return
		}

		go app.updateChecker.CheckNow()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "checking"})
	}))

	var fallbackHandler http.Handler
	if *uiDir != "" {
		fmt.Printf("[ZEAM] Serving UI from %s (override)\n", *uiDir)
		fallbackHandler = http.FileServer(http.Dir(*uiDir))
	} else {
		uiFS, err := fs.Sub(embeddedUI, "ui")
		if err != nil {
			fmt.Printf("[ZEAM] Warning: embedded UI not available: %v\n", err)
		} else {
			fmt.Println("[ZEAM] Serving embedded UI (same-origin mode)")
			fallbackHandler = http.FileServer(http.FS(uiFS))
		}
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		if app.contentPinnedRoot != (content.ContentID{}) {

			data, contentType, err := websitePublisher.GetFile(app.contentPinnedRoot, r.URL.Path)
			if err == nil {
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("X-Content-ID", app.contentPinnedRoot.Hex())
				w.Write(data)
				return
			}

			if r.URL.Path == "/" {
				data, err := app.contentDAG.Get(app.contentPinnedRoot)
				if err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Set("X-Content-ID", app.contentPinnedRoot.Hex())
					w.Write(data)
					return
				}
			}
		}
		if fallbackHandler != nil {
			fallbackHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

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
	fmt.Println("  --- Content/Website Hosting ---")
	fmt.Println("  POST /content/publish      - Publish single file")
	fmt.Println("  POST /content/publish-site - Publish website (tar.gz)")
	fmt.Println("  POST /content/pin          - Pin content to serve at /")
	fmt.Println("  GET  /content/{id}         - Get content by ID")
	fmt.Println("  GET  /site/{manifest}/{path} - Serve site files")
	fmt.Println("  --- Pong (L1 Distributed Compute Demo) ---")
	fmt.Println("  GET  /pong/state          - Get game state")
	fmt.Println("  POST /pong/start          - Start new game")
	fmt.Println("  POST /pong/input          - Send player input")
	fmt.Println("  POST /pong/stop           - Stop game")
	fmt.Println("  POST /pong/reset          - Reset game")
	fmt.Println("  --- Arbitrage (Neural Route Explorer) ---")
	fmt.Println("  GET  /arb/stats           - Arbitrage statistics")
	fmt.Println("  GET  /arb/routes          - Recent arbitrage routes")
	fmt.Println("  --- Rewards (Middle-Out Economics) ---")
	fmt.Println("  GET  /rewards/stats       - Reward system statistics")
	fmt.Println("  GET  /rewards/history     - Epoch history")
	fmt.Println("  --- Auto-Update ---")
	fmt.Println("  GET  /update/status       - Update status")
	fmt.Println("  POST /update/check        - Trigger update check")
	fmt.Println()

	if app.unlocked && !*noP2P {
		go app.startP2P(*p2pPeer)
	}

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatal(err)
		}
	}()

	var ipcServer *IpcServer
	if *ipcSocket != "" {
		var err error
		ipcServer, err = StartIpcServer(app, *ipcSocket, *noP2P, *p2pPeer)
		if err != nil {
			log.Printf("[ZEAM] IPC server failed: %v", err)
		} else {
			defer ipcServer.Close()
		}
	}

	fmt.Println("[ZEAM] Ready. Ctrl+C to exit.")

	if *testPasskey != "" && !app.unlocked {
		fmt.Printf("[ZEAM] Auto-unlocking with --test-passkey (len=%d)...\n", len(*testPasskey))
		passkeySecret := []byte(*testPasskey)

		unifiedID, err := identity.NewUnifiedIdentity(identity.UnifiedConfig{
			Mode:       identity.ModeStateless,
			DataDir:    app.dataDir,
			UserSecret: passkeySecret,
		})

		for i := range passkeySecret {
			passkeySecret[i] = 0
		}

		if err != nil {
			fmt.Printf("[ZEAM] ERROR: Failed to derive identity: %v\n", err)
		} else {
			fmt.Printf("[ZEAM] Identity derived! Address=%s\n", unifiedID.Address.Hex())
			app.mu.Lock()
			app.unifiedID = unifiedID
			app.id = &identity.Identity{
				PrivateKey: unifiedID.PrivateKey,
				Address:    unifiedID.Address,
				ForkID:     unifiedID.ForkID,
				ShortID:    unifiedID.ShortID,
			}
			app.unlocked = true
			app.mu.Unlock()

			if !*noP2P {
				go app.startP2P(*p2pPeer)
			} else {
				fmt.Println("[ZEAM] P2P disabled (--no-p2p flag)")
			}
		}
	}

	if !app.unlocked {
		fmt.Println("[ZEAM] Waiting for passkey via UI...")
		fmt.Println("[ZEAM] TIP: Use --test-passkey=yourpasskey to skip UI")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n[ZEAM] Shutting down...")
	app.mu.RLock()
	if app.pongEngine != nil {
		app.pongEngine.Stop()
	}
	if app.flowOrchestrator != nil {
		app.flowOrchestrator.Stop()
	}
	if app.arbDetector != nil {
		app.arbDetector.Stop()
	}
	if app.multiChain != nil {
		app.multiChain.Stop()
	}
	if app.p2pTransport != nil {
		app.p2pTransport.Stop()
	}
	if app.hybridTransport != nil {
		app.hybridTransport.Stop()
	}

	if app.storageTracker != nil {
		app.storageTracker.Stop()
	}
	if app.epochManager != nil {
		app.epochManager.Stop()
	}
	if app.poolClient != nil {
		app.poolClient.Close()
	}

	if app.updateChecker != nil {
		app.updateChecker.Stop()
	}
	app.mu.RUnlock()
}

func (app *App) startP2P(bootstrapPeer string) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n[ZEAM] PANIC in startP2P: %v\n", r)
			crashLog := fmt.Sprintf("startP2P PANIC at %s: %v\n", time.Now().Format(time.RFC3339), r)
			if f, err := os.OpenFile("zeam_crash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.WriteString(crashLog)
				f.Close()
			}
		}
	}()

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

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n[ZEAM] PANIC in P2P startup: %v\n", r)
			fmt.Println("[ZEAM] P2P disabled due to crash - app continues without P2P")

			crashLog := fmt.Sprintf("P2P PANIC at %s: %v\n", time.Now().Format(time.RFC3339), r)
			if f, err := os.OpenFile("zeam_crash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.WriteString(crashLog)
				f.Close()
			}
		}
	}()

	app.mu.RLock()
	id := app.id
	useTestnet := app.useTestnet
	app.mu.RUnlock()

	if id == nil {
		return
	}

	fmt.Println("[ZEAM] Starting multi-chain parasitic transport...")
	fmt.Println("[ZEAM] Using IPFS relay network for NAT traversal")

	fmt.Println("[ZEAM] Mode: ALL NETWORKS (mainnet + testnet for maximum node coverage)")
	enabledChains := []uint64{

		arb.ChainIDEthereum,
		arb.ChainIDSepolia,

		arb.ChainIDBase,
		arb.ChainIDOptimism,
		arb.ChainIDZora,
		arb.ChainIDMode,
		arb.ChainIDFraxtal,
		arb.ChainIDWorldcoin,
		arb.ChainIDCyber,
		arb.ChainIDMint,
		arb.ChainIDRedstone,
		arb.ChainIDLisk,

		arb.ChainIDBaseSep,
		arb.ChainIDOptimSep,
		arb.ChainIDZoraSep,
		arb.ChainIDModeSep,
	}

	multiChainConfig := &node.MultiChainConfig{
		PrivateKey:    id.PrivateKey,
		EnabledChains: enabledChains,
		UseTestnet:    false,
		MaxPeers:      50,
		ListenPorts: map[uint64]string{
			arb.ChainIDEthereum: ":30313",
			arb.ChainIDSepolia:  ":30314",
		},
	}

	multiChain, err := node.NewMultiChainTransport(multiChainConfig)
	if err != nil {
		fmt.Printf("[ZEAM] Failed to create multi-chain transport: %v\n", err)
		return
	}

	qs := quantum.GetService()
	qs.ConnectFeed(multiChain.Feed())

	multiChain.OnTxReceived = func(chainID uint64, hashes []common.Hash) {
		app.mu.RLock()
		backend := app.backend
		app.mu.RUnlock()

		if backend != nil {

			pressure := float64(len(hashes)) / 100.0
			if pressure > 1.0 {
				pressure = 1.0
			}
			backend.AddPressure("pending", pressure)

			var totalPeers int
			for _, cid := range enabledChains {
				if status := multiChain.GetChainStatus(cid); status != nil {
					totalPeers += status.PeerCount
				}
			}

			blockHash := make([]byte, 8)
			if len(hashes) > 0 {
				copy(blockHash, hashes[0][:8])
			}
			backend.UpdateNetworkState(totalPeers, len(enabledChains), blockHash)
		}
	}

	multiChain.OnPeerEvent = func(chainID uint64, peerID string, connected bool) {

		app.mu.RLock()
		backend := app.backend
		app.mu.RUnlock()

		action := "connected"
		if !connected {
			action = "disconnected"
		}
		fmt.Printf("[ZEAM] Chain %d peer %s: %s\n", chainID, action, peerID[:16])

		if backend != nil {
			var totalPeers int
			for _, cid := range enabledChains {
				if status := multiChain.GetChainStatus(cid); status != nil {
					totalPeers += status.PeerCount
				}
			}
			backend.UpdateNetworkState(totalPeers, len(enabledChains), nil)
		}
	}

	if err := multiChain.Start(); err != nil {
		fmt.Printf("[ZEAM] Failed to start multi-chain transport: %v\n", err)
		return
	}

	app.mu.Lock()
	app.multiChain = multiChain
	app.mu.Unlock()

	var chainID *big.Int
	if useTestnet {
		chainID = big.NewInt(11155111)
	} else {
		chainID = big.NewInt(1)
	}
	app.backend.EnableCollapseCompute(chainID, id.Address)
	fmt.Println("[ZEAM] Collapse compute: ENABLED")

	multiHopConfig := &arb.MultiHopConfig{
		MinProfitBPS:         10,
		MaxHops:              4,
		GraphRebuildInterval: 30 * time.Second,
		RouteScanInterval:    5 * time.Second,
		EnabledChains:        enabledChains,
	}

	flowCompute := multiChain.GetFlowCompute()

	if app.backend != nil {
		flowAdapter := &simpleFlowAdapter{compute: flowCompute}
		app.backend.SetFlowCognition(flowAdapter, flowAdapter)
		fmt.Println("[ZEAM] Flow cognition wired to backend - V3 generation active")
	}

	var ngacBridge *synthesis.NGACBridge
	if app.backend != nil {
		ngacBridge = app.backend.GetBridge()
	}

	arbDetector := arb.NewMultiHopDetector(multiHopConfig, flowCompute, ngacBridge)

	multiChain.OnTxParsed = func(chainID uint64, tx *types.Transaction) {
		arbDetector.ProcessTransaction(chainID, tx)
	}

	arbDetector.OnRouteDiscovered = func(route *routing.ArbitrageRoute) {
		app.mu.RLock()
		backend := app.backend
		app.mu.RUnlock()
		if backend != nil {

			profitMagnitude := float64(route.ProfitBPS) / 1000.0
			if profitMagnitude > 1.0 {
				profitMagnitude = 1.0
			}
			backend.AddPressure("block", profitMagnitude)
			fmt.Printf("[ZEAM] Arb opportunity: %d bps profit -> NGAC pressure %.2f\n",
				route.ProfitBPS, profitMagnitude)
		}
	}

	if err := arbDetector.Start(); err != nil {
		fmt.Printf("[ZEAM] Warning: failed to start arb detector: %v\n", err)
	} else {
		fmt.Println("[ZEAM] Multi-hop arbitrage detector started (feeds NGAC pressure)")
	}

	app.mu.Lock()
	app.arbDetector = arbDetector
	app.mu.Unlock()

	if priceAgg := multiChain.GetPriceAggregator(); priceAgg != nil {
		priceAgg.WireToPoolRegistry(arbDetector.GetPoolRegistry())
		fmt.Println("[ZEAM] Price gossip wired to pool registry - reserves update from P2P")
	}

	multiChain.OnPriceGossip = func(update *node.PriceUpdate) {
		app.mu.RLock()
		backend := app.backend
		app.mu.RUnlock()

		if backend != nil {

			backend.AddPressure("block", 0.1)
		}
	}

	parser := arb.NewMempoolParser()
	priceFeed := arb.NewPriceFeed(multiChain, nil)
	priceFeed.Start()
	flashblocks := arb.NewFlashblocksClient(priceFeed, nil)

	orch := arb.NewFlowOrchestrator(
		multiChain.Feed(), parser, qs,
		arb.NewOpportunityQueue(nil),
		flashblocks, nil,
	)
	orch.RegisterStrategy(arb.NewArbStrategy(priceFeed, nil))
	orch.RegisterStrategy(arb.NewCrossChainStrategy(priceFeed, nil))
	orch.RegisterStrategy(arb.NewLiquidationStrategy(priceFeed, nil))
	orch.RegisterStrategy(arb.NewBackrunStrategy(priceFeed, nil))

	orch.OnProfit = func(profit *big.Int) {
		if app.epochManager != nil {
			app.epochManager.AddRevenue(profit)
			fmt.Printf("[ZEAM] Arb profit %s wei -> epoch manager\n", profit.String())
		}
	}

	if err := orch.Start(); err != nil {
		fmt.Printf("[ZEAM] Warning: flow orchestrator failed to start: %v\n", err)
	} else {
		fmt.Println("[ZEAM] Flow orchestrator started (arb, crosschain, liquidation, backrun)")
	}

	app.mu.Lock()
	app.flowOrchestrator = orch
	app.mu.Unlock()

	fmt.Println("[ZEAM] Multi-chain flow cognition active (all chains feed unified entropy)")

	fmt.Printf("[ZEAM] Multi-chain transport ready\n")
	fmt.Printf("[ZEAM] Address: %s\n", id.Address.Hex())
	if libp2pInfo := multiChain.GetLibP2PInfo(); libp2pInfo["enabled"].(bool) {
		fmt.Printf("[ZEAM] LibP2P Peer ID: %s\n", libp2pInfo["peer_id"].(string)[:16])
	}
	fmt.Printf("[ZEAM] Chains: %v\n", enabledChains)
	fmt.Printf("[ZEAM] Arbitrage detection: ENABLED (feeds NGAC pressure)\n")

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:

				var totalPeers, totalTx int
				for _, cid := range enabledChains {
					if status := multiChain.GetChainStatus(cid); status != nil {
						totalPeers += status.PeerCount
						totalTx += int(status.TxCount)
					}
				}

				app.mu.RLock()
				backend := app.backend
				app.mu.RUnlock()

				if backend != nil {
					pseudoBlockHash := make([]byte, 8)
					pseudoBlockHash[0] = byte(totalPeers)
					pseudoBlockHash[1] = byte(len(enabledChains))
					pseudoBlockHash[2] = byte(time.Now().Unix())
					backend.UpdateNetworkState(totalPeers, len(enabledChains), pseudoBlockHash)
				}

				arbStats := arbDetector.Stats()
				fmt.Printf("[ZEAM] Peers: %d | Tx: %d | Routes: %d | Best: %d bps\n",
					totalPeers, totalTx, arbStats.RoutesFound, arbStats.BestProfitBPS)
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
