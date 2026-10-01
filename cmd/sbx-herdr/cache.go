package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"time"

	"github.com/dvdksn/sbx-herdr/internal/herdr"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

type inventoryCache struct {
	client     *sandbox.Client
	items      []sandbox.Info
	generation uint64
	next       time.Time
	err        error
}

func (cache *inventoryCache) get(ctx context.Context, now time.Time, force bool) ([]sandbox.Info, error) {
	if now.Before(cache.next) && (!force || cache.err != nil) {
		return cache.items, cache.err
	}
	cache.generation++
	cache.items, cache.err = nil, nil
	if cache.client == nil {
		binary := os.Getenv("SBX_HERDR_SBX")
		if binary == "" {
			binary = "sbx"
		}
		discovery, cancel := context.WithTimeout(ctx, 5*time.Second)
		socket, err := sandbox.Discover(discovery, binary, os.Getenv("SBX_HERDR_SOCKET"))
		cancel()
		cache.err = err
		if err == nil {
			cache.client = sandbox.New(socket)
			cache.err = cache.client.Health(ctx)
		}
	}
	if cache.err == nil {
		cache.items, cache.err = cache.client.List(ctx)
	}
	if cache.err != nil {
		cache.items = nil
		if cache.client != nil {
			cache.client.HTTP.CloseIdleConnections()
		}
		cache.client = nil
	}
	cache.next = time.Now().Add(10 * time.Second)
	return cache.items, cache.err
}

type paneObservation struct {
	fingerprint [32]byte
	command     invocation
	matched     bool
	name        string
	generation  uint64
	info        sandbox.Info
	agent       string
	resolved    bool
}

func observeProcesses(previous paneObservation, processes []herdr.Process) (paneObservation, bool) {
	data, _ := json.Marshal(processes)
	fingerprint := sha256.Sum256(data)
	if fingerprint == previous.fingerprint {
		return previous, false
	}
	command, matched := detect(processes)
	return paneObservation{fingerprint: fingerprint, command: command, matched: matched, name: sandboxName(command)}, true
}

type reportState struct {
	command                       invocation
	agent, screen, state, sandbox string
	classified                    time.Time
	metadata                      time.Time
	reported                      bool
}
