package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

type Client struct {
	Binary, Socket, Pane, Source string
	Dial                         func(context.Context) (net.Conn, error)
}

func FromEnvironment() Client {
	binary := os.Getenv("HERDR_BIN_PATH")
	if binary == "" {
		binary = "herdr"
	}
	return Client{Binary: binary, Socket: os.Getenv("HERDR_SOCKET_PATH"), Pane: os.Getenv("HERDR_PANE_ID"), Source: "dvdksn.sbx-herdr"}
}

func (client Client) Request(ctx context.Context, method string, params any, kind string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	dial := client.Dial
	if dial == nil {
		dial = func(ctx context.Context) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", client.Socket)
		}
	}
	connection, err := dial(ctx)
	if err != nil {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return err
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := json.NewEncoder(connection).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	var response struct {
		ID     string                          `json:"id"`
		Result json.RawMessage                 `json:"result"`
		Error  *struct{ Code, Message string } `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(connection, 4<<20)).Decode(&response); err != nil {
		return err
	}
	if response.ID != id {
		return fmt.Errorf("herdr %s: response ID mismatch", method)
	}
	if response.Error != nil {
		return fmt.Errorf("herdr %s: %s: %s", method, response.Error.Code, response.Error.Message)
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(response.Result, &header); err != nil {
		return err
	}
	if header.Type == "" || kind != "" && header.Type != kind {
		return fmt.Errorf("herdr %s: unexpected result type %q", method, header.Type)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(response.Result, result)
}

func (client Client) reportParams(agent string) map[string]any {
	return map[string]any{"pane_id": client.Pane, "source": client.Source, "agent": "sbx-" + agent, "seq": time.Now().UnixNano()}
}

func (client Client) Metadata(ctx context.Context, key, value string) error {
	var token any
	if value != "" {
		token = value
	}
	return client.Request(ctx, "pane.report_metadata", map[string]any{
		"pane_id": client.Pane, "source": client.Source, "seq": time.Now().UnixNano(),
		"ttl_ms": 15000, "tokens": map[string]any{key: token},
	}, "", nil)
}

func (client Client) Report(ctx context.Context, agent, state string) error {
	params := client.reportParams(agent)
	params["state"] = state
	return client.Request(ctx, "pane.report_agent", params, "", nil)
}

func (client Client) Release(ctx context.Context, agent string) error {
	return client.Request(ctx, "pane.release_agent", client.reportParams(agent), "", nil)
}

func (client Client) Screen(ctx context.Context) (string, error) {
	var result struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err := client.Request(ctx, "pane.read", map[string]any{"pane_id": client.Pane, "source": "detection", "format": "text", "strip_ansi": true}, "pane_read", &result)
	return result.Read.Text, err
}

func (client Client) Classify(ctx context.Context, agent, screen string) (string, error) {
	file, err := os.CreateTemp("", "sbx-herdr-screen-*")
	if err != nil {
		return "unknown", err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(screen); err != nil {
		file.Close()
		return "unknown", err
	}
	if err := file.Close(); err != nil {
		return "unknown", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, client.Binary, "agent", "explain", "--file", file.Name(), "--agent", agent, "--json").Output()
	if err != nil {
		return "unknown", err
	}
	return Classification(data)
}

func Classification(data []byte) (string, error) {
	var result struct {
		State   string `json:"state"`
		Matched any    `json:"matched_rule"`
		Skip    bool   `json:"skip_state_update"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "unknown", err
	}
	if result.Skip || result.Matched == nil {
		return "unknown", nil
	}
	switch result.State {
	case "idle", "working", "blocked":
		return result.State, nil
	}
	return "unknown", nil
}

type Pane struct {
	ID    string `json:"pane_id"`
	Agent string `json:"agent"`
}

type Process struct {
	PID  int      `json:"pid"`
	Argv []string `json:"argv"`
	CWD  string   `json:"cwd"`
}

func (client Client) Panes(ctx context.Context) ([]Pane, error) {
	var result struct {
		Panes []Pane `json:"panes"`
	}
	err := client.Request(ctx, "pane.list", map[string]any{}, "pane_list", &result)
	return result.Panes, err
}

func (client Client) Processes(ctx context.Context) ([]Process, error) {
	var result struct {
		Info struct {
			Processes []Process `json:"foreground_processes"`
		} `json:"process_info"`
	}
	err := client.Request(ctx, "pane.process_info", map[string]any{"pane_id": client.Pane}, "pane_process_info", &result)
	return result.Info.Processes, err
}

func (client Client) Enabled(ctx context.Context) (bool, error) {
	var result struct {
		Plugins []struct {
			ID      string `json:"plugin_id"`
			Enabled bool   `json:"enabled"`
		} `json:"plugins"`
	}
	if err := client.Request(ctx, "plugin.list", map[string]any{"plugin_id": "dvdksn.sbx-herdr"}, "plugin_list", &result); err != nil {
		return false, err
	}
	for _, plugin := range result.Plugins {
		if plugin.ID == "dvdksn.sbx-herdr" {
			return plugin.Enabled, nil
		}
	}
	return false, nil
}
