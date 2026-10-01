package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dvdksn/sbx-herdr/internal/attachment"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sbx-herdr-guest:", err)
		if status, ok := err.(*exec.ExitError); ok {
			os.Exit(status.ExitCode())
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 2 && args[0] == "read" {
		path, err := attachment.Path(args[1])
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 4097))
		if err != nil {
			return err
		}
		if len(data) > 4096 {
			return fmt.Errorf("attachment report too large")
		}
		_, err = os.Stdout.Write(data)
		return err
	}
	if len(args) < 2 || args[0] != "exec" {
		return fmt.Errorf("usage: sbx-herdr-guest exec COMMAND [ARG...] | read ATTACHMENT")
	}
	token := os.Getenv("SBX_HERDR_ATTACHMENT")
	if token == "" || os.Getenv("HERDR_ENV") == "1" {
		path, err := exec.LookPath(args[1])
		if err != nil {
			return err
		}
		return syscall.Exec(path, args[1:], os.Environ())
	}
	path, err := attachment.Path(token)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	command := exec.Command(args[1], args[2:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)
	defer signal.Stop(signals)
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case sig := <-signals:
			_ = command.Process.Signal(sig)
		case <-ticker.C:
			state := attachment.State{Version: 1, Attachment: token, Agent: foregroundAgent("/proc"), Updated: time.Now().UnixMilli()}
			if err := attachment.Write(state); err != nil {
				fmt.Fprintln(os.Stderr, "sbx-herdr-guest: reporting unavailable:", err)
			}
		}
	}
}

func processStat(data string) (tty, group, foreground string, ok bool) {
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 6 {
		return
	}
	return fields[4], fields[2], fields[5], true
}

func foregroundAgent(root string) string {
	self, err := os.ReadFile(root + "/self/stat")
	if err != nil {
		return ""
	}
	tty, _, _, ok := processStat(string(self))
	if !ok || tty == "0" {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	found := ""
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(root + "/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		otherTTY, group, foreground, ok := processStat(string(data))
		if !ok || otherTTY != tty || group != foreground {
			continue
		}
		command, err := os.ReadFile(root + "/" + entry.Name() + "/cmdline")
		if err != nil {
			continue
		}
		words := strings.Split(string(command), "\x00")
		agent := attachment.Agent(words[0])
		if agent == "" && len(words) > 1 && (words[0] == "node" || strings.HasSuffix(words[0], "/node")) {
			agent = attachment.Agent(words[1])
		}
		if agent != "" {
			if found != "" && found != agent {
				return ""
			}
			found = agent
		}
	}
	return found
}
