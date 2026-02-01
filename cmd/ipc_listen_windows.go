package main

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func ipcListen(pipePath string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	listener, err := winio.ListenPipe(pipePath, cfg)
	if err != nil {
		return nil, fmt.Errorf("ipc listen pipe: %w", err)
	}
	return listener, nil
}

func ipcCleanup(_ string) {}
