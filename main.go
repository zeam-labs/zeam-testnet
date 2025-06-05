package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"zeam/app"
	"zeam/ipfs"
	"zeam/spawn/agent_spawner"
	"zeam/spawm/presence_spawner"
)

var runtime *app.App
var Chains = map[string]*Chain{}

type Chain struct {
	Log []string
}

func (c *Chain) Mint(ctx context.Context, entry string) {
	c.Log = append(c.Log, entry)
}

func main() {
	Chains["civicL1"] = &Chain{}
	Chains["cognitionL1"] = &Chain{}
	Chains["civicL4"] = &Chain{}
	Chains["cognitionL4"] = &Chain{}
	Chains["civicL6"] = &Chain{}
	Chains["cognitionL6"] = &Chain{}

	ipfs.InitIPFS("localhost:5001")

	runtime = app.NewApp(
		Chains["civicL1"].Log,
		Chains["cognitionL1"].Log,
		Chains["civicL4"].Log,
		Chains["cognitionL4"].Log,
		Chains["civicL6"],
		Chains["cognitionL6"],
	)

	http.HandleFunc("/interpret", handleInterpret)
	http.HandleFunc("/spawn", handleSpawn)

	http.ListenAndServe(":8080", nil)
}

func handleInterpret(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	text := strings.TrimSpace(string(body))
	if text != "" {
		runtime.Interpret(text)
		w.WriteHeader(http.StatusOK)
	}
}

func handleSpawn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	typ := strings.TrimSpace(string(body))
	switch typ {
	case "presence":
		presence_spawner.SpawnPresence()
	case "agent":
		agent_spawner.SpawnAgent()
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}
