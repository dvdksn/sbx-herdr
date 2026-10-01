package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dvdksn/sbx-herdr/internal/herdr"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sbx-herdr:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("internal Herdr plugin helper; use sbx normally in a Herdr pane")
	}
	switch os.Args[1] {
	case "--help":
		fmt.Println("sbx-herdr is launched automatically by Herdr. Use sbx run or sbx exec, not this binary, to attach.")
		return nil
	case "ensure", "refresh":
		return ensure()
	case "observe":
		lock := os.NewFile(3, "observer-lock")
		if lock == nil {
			return fmt.Errorf("observe requires the inherited plugin lock")
		}
		defer lock.Close()
		if _, err := lock.Stat(); err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer cancel()
		return observe(ctx)
	default:
		return fmt.Errorf("use sbx directly; this binary only runs the Herdr observer")
	}
}

func ensure() error {
	directory, socket := os.Getenv("HERDR_PLUGIN_STATE_DIR"), os.Getenv("HERDR_SOCKET_PATH")
	if directory == "" || socket == "" {
		return fmt.Errorf("must be launched by Herdr with plugin state and socket context")
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(socket)))[:16]
	lock, err := os.OpenFile(filepath.Join(directory, "observer-"+key+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(directory, "observer-"+key+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(binary, "observe")
	command.ExtraFiles = []*os.File{lock}
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type observer struct {
	host     herdr.Client
	agents   map[string]string
	bindings map[string]binding
	reports  map[string]reportState
}

type binding struct {
	command invocation
	id      string
}

func (watcher *observer) clear(ctx context.Context, pane string) error {
	host := watcher.host
	host.Pane = pane
	if agent := watcher.agents[pane]; agent != "" {
		if err := host.Release(ctx, agent); err != nil {
			return err
		}
		delete(watcher.agents, pane)
	}
	delete(watcher.reports, pane)
	return host.Metadata(ctx, "sbx", "")
}

func observe(ctx context.Context) error {
	watcher := observer{host: herdr.FromEnvironment(), agents: make(map[string]string)}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for pane := range watcher.agents {
			_ = watcher.clear(cleanup, pane)
		}
	}()
	var inventory inventoryCache
	observations := make(map[string]paneObservation)
	lastWarning := time.Time{}
	warn := func(err error) {
		if err != nil && ctx.Err() == nil && time.Since(lastWarning) > time.Minute {
			fmt.Fprintln(os.Stderr, time.Now().Format(time.RFC3339), err)
			lastWarning = time.Now()
		}
	}
	for ctx.Err() == nil {
		enabled, err := watcher.host.Enabled(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		panes, err := watcher.host.Panes(ctx)
		if err != nil {
			return err
		}
		commands := make(map[string]paneObservation)
		changed := false
		present := make(map[string]bool)
		for _, pane := range panes {
			present[pane.ID] = true
			host := watcher.host
			host.Pane = pane.ID
			if watcher.agents[pane.ID] == "" && strings.HasPrefix(pane.Agent, "sbx-") {
				watcher.agents[pane.ID] = strings.TrimPrefix(pane.Agent, "sbx-")
			}
			if previous, exists := watcher.reports[pane.ID]; exists && pane.Agent != "sbx-"+previous.agent {
				previous.reported = false
				watcher.reports[pane.ID] = previous
			}
			processes, err := host.Processes(ctx)
			observation, processChanged := observeProcesses(observations[pane.ID], processes)
			if err == nil {
				observations[pane.ID] = observation
			} else {
				delete(observations, pane.ID)
			}
			if err == nil && observation.matched {
				if observation.command.Mode == "env-run" && observation.name == "" {
					warn(watcher.report(ctx, pane.ID, observation.command, sandbox.Info{}, "env"))
				} else {
					commands[pane.ID] = observation
					changed = changed || processChanged
				}
			} else if watcher.agents[pane.ID] != "" {
				warn(watcher.clear(ctx, pane.ID))
			}
			if err == nil && !observation.matched {
				delete(watcher.bindings, pane.ID)
			}
			warn(err)
		}
		for pane := range watcher.agents {
			if !present[pane] {
				delete(watcher.agents, pane)
			}
		}
		for pane := range watcher.bindings {
			if !present[pane] {
				delete(watcher.bindings, pane)
			}
		}
		for pane := range observations {
			if !present[pane] {
				delete(observations, pane)
				delete(watcher.reports, pane)
			}
		}
		if len(commands) > 0 {
			items, err := inventory.get(ctx, time.Now(), changed)
			warn(err)
			for pane, observation := range commands {
				if observation.generation != inventory.generation {
					lookup := observation.command
					lookup.Name = observation.name
					observation.info, observation.agent, observation.resolved = resolve(lookup, items)
					observation.generation = inventory.generation
					observations[pane] = observation
				}
				if !observation.resolved {
					if observation.command.Mode == "env-run" {
						warn(watcher.report(ctx, pane, observation.command, sandbox.Info{}, "env"))
					} else if watcher.agents[pane] != "" {
						warn(watcher.clear(ctx, pane))
					}
					continue
				}
				warn(watcher.report(ctx, pane, observation.command, observation.info, observation.agent))
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func (watcher *observer) report(ctx context.Context, pane string, command invocation, info sandbox.Info, agent string) error {
	host := watcher.host
	host.Pane = pane
	if watcher.bindings == nil {
		watcher.bindings = make(map[string]binding)
	}
	if previous, exists := watcher.bindings[pane]; exists && previous.command == command && previous.id != "" && previous.id != info.ID {
		return watcher.clear(ctx, pane)
	}
	watcher.bindings[pane] = binding{command: command, id: info.ID}
	if watcher.reports == nil {
		watcher.reports = make(map[string]reportState)
	}
	previous := watcher.reports[pane]
	if previous.command != command || previous.agent != agent {
		previous = reportState{command: command, agent: agent}
	}
	next := previous
	state := "unknown"
	if agent != "shell" && agent != "env" {
		screen, err := host.Screen(ctx)
		if err == nil {
			if screen == previous.screen && time.Since(previous.classified) < 30*time.Second {
				state = previous.state
			} else {
				state, err = host.Classify(ctx, agent, screen)
				if err == nil {
					next.screen, next.classified = screen, time.Now()
				}
			}
		}
		if err != nil {
			state = "unknown"
			next.classified = time.Time{}
		}
	}
	next.state = state
	reportChanged := !previous.reported || previous.state != state
	metadataDue := previous.sandbox != info.Name || time.Since(previous.metadata) >= 10*time.Second
	if reportChanged || metadataDue {
		processes, err := host.Processes(ctx)
		if err != nil {
			return watcher.clear(ctx, pane)
		}
		current, matched := detect(processes)
		if !matched || current != command {
			return watcher.clear(ctx, pane)
		}
		if old := watcher.agents[pane]; old != "" && old != agent {
			if err := watcher.clear(ctx, pane); err != nil {
				return err
			}
		}
		if reportChanged {
			if err := host.Report(ctx, agent, state); err != nil {
				return err
			}
			watcher.agents[pane] = agent
			next.reported = true
		}
		if metadataDue {
			if err := host.Metadata(ctx, "sbx", info.Name); err != nil {
				return err
			}
			next.metadata, next.sandbox = time.Now(), info.Name
		}
	}
	watcher.reports[pane] = next
	return nil
}
