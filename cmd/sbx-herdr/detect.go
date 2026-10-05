package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/dvdksn/sbx-herdr/internal/attachment"
	"github.com/dvdksn/sbx-herdr/internal/herdr"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

var envNamePattern = regexp.MustCompile(`(?m)^name:[ \t]*["']?([A-Za-z0-9][A-Za-z0-9_.-]*)["']?[ \t]*(?:#.*)?$`)

type invocation struct {
	PID                                 int
	Mode, Name, Agent, Workspace, Files string
}

func detect(processes []herdr.Process) (invocation, bool) {
	var found invocation
	count := 0
	for _, process := range processes {
		if candidate, ok := parse(process); ok {
			found = candidate
			count++
		}
	}
	return found, count == 1
}

func parse(process herdr.Process) (invocation, bool) {
	result := invocation{PID: process.PID, Workspace: process.CWD}
	args := process.Argv
	if len(args) < 2 || filepath.Base(args[0]) != "sbx" {
		return result, false
	}
	args = args[1:]
	for len(args) > 0 && (args[0] == "--debug" || args[0] == "-D" || args[0] == "--debug=true" || args[0] == "--debug=false") {
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "env" {
		return parseEnvRun(result, args[1:])
	}
	if len(args) == 0 || (args[0] != "run" && args[0] != "exec") {
		return result, false
	}
	result.Mode, args = args[0], args[1:]
	var positional []string
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			if result.Mode == "exec" {
				positional = append(positional, args...)
			}
			break
		}
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			if result.Mode == "exec" {
				positional = append(positional, args...)
				break
			}
			continue
		}
		flag, value, assigned := strings.Cut(arg, "=")
		switch flag {
		case "--cloud", "--detached", "--detach", "-d", "--help", "-h":
			if !assigned || value != "false" {
				return result, false
			}
			continue
		case "--debug", "-D", "--clone", "--privileged", "--interactive", "--tty", "-i", "-it", "-ti":
			continue
		}
		if flag == "-t" && result.Mode == "exec" {
			continue
		}
		if !strings.HasPrefix(flag, "--") && len(flag) > 2 && strings.Contains("euwmtp", flag[1:2]) {
			value, flag, assigned = arg[2:], arg[:2], true
		}
		switch flag {
		case "--name", "--env", "-e", "--env-file", "--user", "-u", "--workdir", "-w", "--detach-keys",
			"--cpus", "--memory", "-m", "--kit", "--kit-arg", "--kit-args-file", "--template", "-t",
			"--profile", "--publish", "-p", "--pull", "--skills", "--static-mcp", "--deny-network":
			if !assigned {
				if len(args) == 0 {
					return result, false
				}
				value, args = args[0], args[1:]
			}
			if flag == "--name" {
				result.Name = value
			}
		default:
			return result, false
		}
	}
	if result.Mode == "exec" {
		if len(positional) < 2 {
			return result, false
		}
		result.Name = positional[0]
		result.Agent = label(positional[1])
		return result, true
	}
	if len(positional) > 0 {
		result.Agent = positional[0]
	}
	if len(positional) > 1 {
		result.Workspace = positional[1]
		if !filepath.IsAbs(result.Workspace) {
			result.Workspace = filepath.Join(process.CWD, result.Workspace)
		}
	}
	return result, result.Name != "" || result.Agent != ""
}

func parseEnvRun(result invocation, args []string) (invocation, bool) {
	if len(args) == 0 || args[0] != "run" {
		return result, false
	}
	result.Mode, result.Agent = "env-run", "env"
	args = args[1:]
	var paths []string
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if arg == "--" {
			paths = append(paths, args...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
			continue
		}
		flag, value, assigned := strings.Cut(arg, "=")
		switch flag {
		case "--cloud", "--detached", "-d", "--help", "-h":
			if !assigned || value != "false" {
				return result, false
			}
		case "--auto-approve", "-y", "--clone", "--skip-host-commands", "--debug", "-D":
		case "--name", "--env-arg", "--env-args-file", "--kit-arg", "--kit-args-file":
			if !assigned {
				if len(args) == 0 {
					return result, false
				}
				value, args = args[0], args[1:]
			}
			if flag == "--name" {
				result.Name = value
			}
		default:
			return result, false
		}
	}
	result.Files = strings.Join(paths, "\x00")
	return result, true
}

func sandboxName(command invocation) string {
	if command.Mode != "env-run" || command.Name != "" || command.Workspace == "" {
		return command.Name
	}
	paths := []string{"."}
	if command.Files != "" {
		paths = strings.Split(command.Files, "\x00")
	}
	name := ""
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(command.Workspace, path)
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path = filepath.Join(path, "sbxenv.yaml")
		}
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			file.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, 1<<20))
		file.Close()
		if err != nil {
			continue
		}
		if match := envNamePattern.FindSubmatch(data); match != nil {
			name = string(match[1])
		}
	}
	return name
}

func label(command string) string {
	if agent := attachment.Agent(command); agent != "" {
		return agent
	}
	return "shell"
}

func resolve(command invocation, inventory []sandbox.Info) (sandbox.Info, string, bool) {
	var found sandbox.Info
	count := 0
	for _, info := range inventory {
		if info.ID == "" || info.Status != "running" {
			continue
		}
		matches := command.Name != "" && info.Name == command.Name
		if command.Name == "" && command.Mode == "run" && attachment.Normalize(info.Agent) == attachment.Normalize(command.Agent) && command.Workspace != "" && info.Workspace != "" {
			workspace, workspaceErr := filepath.EvalSymlinks(command.Workspace)
			mounted, mountedErr := filepath.EvalSymlinks(info.Workspace)
			matches = workspaceErr == nil && mountedErr == nil && workspace == mounted
		}
		if matches {
			found = info
			count++
		}
	}
	agent := command.Agent
	if command.Mode == "run" || command.Mode == "env-run" {
		agent = label(found.Agent)
	}
	return found, agent, count == 1
}
