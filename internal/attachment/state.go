package attachment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const Helper = "/usr/local/bin/sbx-herdr-guest"

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

type State struct {
	Version    int    `json:"version"`
	Attachment string `json:"attachment"`
	Agent      string `json:"agent"`
	Updated    int64  `json:"updated"`
}

func Decode(data []byte, token string, now time.Time) (string, error) {
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return "", err
	}
	if state.Version != 1 || state.Attachment != token {
		return "", fmt.Errorf("attachment report identity/version mismatch")
	}
	age := now.UnixMilli() - state.Updated
	if age < -1000 || age >= 5000 {
		return "", fmt.Errorf("attachment report is stale or has an invalid timestamp")
	}
	if state.Agent != "" && Agent(state.Agent) != state.Agent {
		return "", fmt.Errorf("unrecognized reported agent")
	}
	return state.Agent, nil
}

func Path(token string) (string, error) {
	if !tokenPattern.MatchString(token) {
		return "", fmt.Errorf("invalid attachment ID")
	}
	return filepath.Join("/tmp", "sbx-herdr-"+token+".json"), nil
}

func Agent(command string) string {
	switch filepath.Base(command) {
	case "claude", "codex", "opencode", "pi", "cursor", "copilot", "devin", "droid", "gemini", "kiro":
		return filepath.Base(command)
	default:
		return ""
	}
}

func Write(state State) error {
	path, err := Path(state.Attachment)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sbx-herdr-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
