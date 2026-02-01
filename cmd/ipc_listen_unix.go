package main

import (
	"fmt"
	"net"
	"os"
)

func ipcListen(socketPath string) (net.Listener, error) {
	os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("ipc listen: %w", err)
	}
	return listener, nil
}

func ipcCleanup(socketPath string) {
	os.Remove(socketPath)
}
