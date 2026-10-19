package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

type savedMachine struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

type registration struct {
	out       io.Writer
	command   func(bool, string, ...string) ([]byte, error)
	inventory func() ([]sandbox.Info, error)
	herdr     string
	sbx       string
}

func registrationAction(action string) error {
	binary := os.Getenv("HERDR_BIN_PATH")
	if binary == "" {
		binary = "herdr"
	}
	cmd := exec.Command(binary, "plugin", "pane", "open", "--plugin", "dvdksn.sbx-herdr", "--entrypoint", action, "--focus")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func newRegistration() registration {
	r := registration{out: os.Stdout, herdr: os.Getenv("HERDR_BIN_PATH"), sbx: os.Getenv("SBX_HERDR_SBX")}
	if r.herdr == "" {
		r.herdr = "herdr"
	}
	if r.sbx == "" {
		r.sbx = "sbx"
	}
	r.command = func(capture bool, binary string, args ...string) ([]byte, error) {
		cmd := exec.Command(binary, args...)
		cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
		if capture {
			return cmd.Output()
		}
		cmd.Stdout = os.Stdout
		return nil, cmd.Run()
	}
	r.inventory = func() ([]sandbox.Info, error) {
		var cache inventoryCache
		return cache.get(context.Background(), time.Now(), true)
	}
	return r
}

func (r registration) remove(m savedMachine) error {
	machines, err := r.machines()
	if err != nil {
		return err
	}
	for _, current := range machines {
		if current.ID == m.ID && current.Target == m.Target && current.Session == m.Session && sandboxHost.MatchString(current.Target) {
			_, err := r.command(false, r.herdr, "machine", "remove", m.ID)
			return err
		}
	}
	return errors.New("machine registration changed; refresh the list")
}

func (r registration) machines() ([]savedMachine, error) {
	data, err := r.command(true, r.herdr, "machine", "list", "--json")
	if err != nil {
		return nil, err
	}
	var machines []savedMachine
	if err := json.Unmarshal(data, &machines); err != nil {
		return nil, err
	}
	for _, m := range machines {
		if m.ID == "" || m.Target == "" || m.Session == "" {
			return nil, errors.New("invalid Herdr machine inventory")
		}
	}
	return machines, nil
}

var sandboxHost = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*\.sbx$`)

func matchingRegistration(machines []savedMachine, name string) (*savedMachine, error) {
	var found *savedMachine
	for _, m := range machines {
		if m.Target != name+".sbx" || m.Session != "default" {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple Herdr machines target %s", name)
		}
		copy := m
		found = &copy
	}
	return found, nil
}

func (r registration) add(selected sandbox.Info) error {
	if selected.ID == "" || !sandboxHost.MatchString(selected.Name+".sbx") {
		return errors.New("invalid sandbox identity")
	}
	items, err := r.inventory()
	if err != nil {
		return err
	}
	found := false
	for _, s := range items {
		if s.Name == selected.Name && s.ID == selected.ID {
			selected = s
			found = true
		}
	}
	if !found {
		return errors.New("sandbox changed; reopen the picker")
	}
	if selected.Status != "running" {
		if _, err := r.command(false, r.sbx, "exec", selected.Name, "true"); err != nil {
			return fmt.Errorf("start sandbox: %w", err)
		}
	}
	machines, err := r.machines()
	if err != nil {
		return err
	}
	m, err := matchingRegistration(machines, selected.Name)
	if err != nil {
		return err
	}
	if m == nil {
		fmt.Fprintln(r.out, "Herdr may ask to install or update its remote server.")
		if _, err := r.command(false, r.herdr, "machine", "add", selected.Name+".sbx", "--label", selected.Name, "--remote-session", "default"); err != nil {
			return err
		}
		machines, err = r.machines()
		if err != nil {
			return err
		}
		m, err = matchingRegistration(machines, selected.Name)
		if err != nil {
			return err
		}
		if m == nil {
			return errors.New("Herdr did not save a machine")
		}
	}
	if !m.Enabled {
		if _, err := r.command(false, r.herdr, "machine", "enable", m.ID); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "Registered: %s. Select its machine in Herdr.\n", selected.Name)
	return nil
}
