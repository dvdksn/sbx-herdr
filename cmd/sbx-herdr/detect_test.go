package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dvdksn/sbx-herdr/internal/herdr"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

type healthTransport struct{}

func (healthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"api_version":"0.36.0"}`)), Header: make(http.Header)}, nil
}

func TestHealthAccepts036(t *testing.T) {
	client := sandbox.Client{HTTP: &http.Client{Transport: healthTransport{}}}
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type rpcRequest struct {
	ID, Method string
	Params     map[string]any
}

func fakeHerdr(t *testing.T, handle func(rpcRequest) map[string]any) herdr.Client {
	t.Helper()
	return herdr.Client{Source: "dvdksn.sbx-herdr", Dial: func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var request rpcRequest
			if err := json.NewDecoder(server).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			response := handle(request)
			response["id"] = request.ID
			if err := json.NewEncoder(server).Encode(response); err != nil {
				t.Error(err)
			}
		}()
		return client, nil
	}}
}

func TestPluginStatusOverSocket(t *testing.T) {
	host := fakeHerdr(t, func(request rpcRequest) map[string]any {
		if request.Method != "plugin.list" {
			t.Errorf("unexpected method %s", request.Method)
		}
		return map[string]any{"result": map[string]any{"type": "plugin_list", "plugins": []any{map[string]any{"plugin_id": "dvdksn.sbx-herdr", "enabled": true}}}}
	})
	enabled, err := host.Enabled(context.Background())
	if err != nil || !enabled {
		t.Fatalf("plugin status: enabled=%v, error=%v", enabled, err)
	}
}

func TestDetect(t *testing.T) {
	for _, test := range []struct {
		args        []string
		name, agent string
		ok          bool
	}{
		{[]string{"sbx", "env", "run"}, "", "env", true},
		{[]string{"sbx", "env", "run", "/home/me/base.yaml", "overlay.yaml", "-y", "--env-arg", "MODE=dev", "--name=demo"}, "", "env", true},
		{[]string{"sbx", "env", "run", "--detached=false", "--skip-host-commands"}, "", "env", true},
		{[]string{"sbx", "env", "run", "--", "-d.yaml"}, "", "env", true},
		{[]string{"sbx", "env", "run", "--detached"}, "", "", false},
		{[]string{"sbx", "env", "run", "base.yaml", "-d"}, "", "", false},
		{[]string{"sbx", "env", "run", "--cloud"}, "", "", false},
		{[]string{"sbx", "env", "run", "--env-arg"}, "", "", false},
		{[]string{"sbx", "env", "plan"}, "", "", false},
		{[]string{"sbx", "env", "exec", "--", "bash"}, "", "", false},
		{[]string{"sbx", "exec", "-it", "demo", "codex"}, "demo", "codex", true},
		{[]string{"/usr/local/bin/sbx", "exec", "-e", "KEY=value", "-u", "agent", "--", "demo", "bash", "-il"}, "demo", "shell", true},
		{[]string{"sbx", "exec", "demo", "sh", "-c", "codex"}, "demo", "shell", true},
		{[]string{"sbx", "run", "--name=demo", "--", "--name", "not-the-sandbox"}, "demo", "", true},
		{[]string{"sbx", "run", "codex", "--name", "demo", "-t", "my-template"}, "demo", "codex", true},
		{[]string{"sbx", "run", "codex", "--detached=false", "--name", "demo"}, "demo", "codex", true},
		{[]string{"sbx", "run", "-d", "codex"}, "", "", false},
		{[]string{"sbx", "--cloud", "run", "--name", "demo"}, "", "", false},
		{[]string{"sbx", "exec", "--cloud", "demo", "codex"}, "", "", false},
		{[]string{"sbx", "run", "--unknown", "demo"}, "", "", false},
		{[]string{"sbx", "exec", "demo"}, "", "", false},
		{[]string{"sbx", "create", "codex"}, "", "", false},
		{[]string{"bash", "-c", "sbx exec demo codex"}, "", "", false},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			got, ok := detect([]herdr.Process{{PID: 10, Argv: test.args}})
			if ok != test.ok || ok && (got.Name != test.name || got.Agent != test.agent) {
				t.Fatalf("got %+v, %v", got, ok)
			}
		})
	}
	process := herdr.Process{Argv: []string{"sbx", "exec", "demo", "codex"}}
	if _, ok := detect([]herdr.Process{process, process}); ok {
		t.Fatal("ambiguous foreground was accepted")
	}
}

func TestResolve(t *testing.T) {
	directory := t.TempDir()
	info := sandbox.Info{Name: "demo", ID: "uuid", Agent: "codex", Status: "running", Workspace: directory}
	command := invocation{Mode: "exec", Name: "demo", Agent: "shell"}
	if _, agent, ok := resolve(command, []sandbox.Info{info}); !ok || agent != "shell" {
		t.Fatal("shell inherited the sandbox's configured agent")
	}
	command = invocation{Mode: "run", Agent: "codex", Workspace: directory}
	if _, agent, ok := resolve(command, []sandbox.Info{info}); !ok || agent != "codex" {
		t.Fatal("unique workspace not matched")
	}
	other := info
	other.Name, other.ID = "other", "other-uuid"
	if _, _, ok := resolve(command, []sandbox.Info{info, other}); ok {
		t.Fatal("ambiguous workspace matched")
	}
	info.Status = "stopped"
	if _, _, ok := resolve(command, []sandbox.Info{info}); ok {
		t.Fatal("stopped sandbox matched")
	}
}

func TestReleaseWhenAttachmentEnds(t *testing.T) {
	var calls []string
	host := fakeHerdr(t, func(request rpcRequest) map[string]any {
		calls = append(calls, request.Method)
		if request.Method == "pane.process_info" {
			return map[string]any{"result": map[string]any{"type": "pane_process_info", "process_info": map[string]any{"foreground_processes": []any{}}}}
		}
		return map[string]any{"result": map[string]any{"type": "ok"}}
	})
	watcher := observer{host: host, agents: map[string]string{"w1:p1": "shell"}}
	if err := watcher.report(context.Background(), "w1:p1", invocation{PID: 10}, sandbox.Info{Name: "demo"}, "shell"); err != nil {
		t.Fatal(err)
	}
	if watcher.agents["w1:p1"] != "" || strings.Join(calls, ",") != "pane.process_info,pane.release_agent,pane.report_metadata" {
		t.Fatalf("stale attachment not released: %v", calls)
	}
}

func TestEnvReportsUnknownWithoutSandbox(t *testing.T) {
	process := herdr.Process{PID: 10, Argv: []string{"sbx", "env", "run", "base.yaml"}}
	command, ok := detect([]herdr.Process{process})
	if !ok {
		t.Fatal("env run not recognized")
	}
	var calls []string
	host := fakeHerdr(t, func(request rpcRequest) map[string]any {
		calls = append(calls, request.Method)
		switch request.Method {
		case "pane.process_info":
			return map[string]any{"result": map[string]any{"type": "pane_process_info", "process_info": map[string]any{"foreground_processes": []herdr.Process{process}}}}
		case "pane.report_agent":
			if request.Params["agent"] != "sbx-env" || request.Params["state"] != "unknown" {
				t.Errorf("unexpected report: %v", request.Params)
			}
		case "pane.report_metadata":
			if request.Params["tokens"].(map[string]any)["sbx"] != nil {
				t.Error("env run claimed a sandbox")
			}
		default:
			t.Errorf("unexpected method: %s", request.Method)
		}
		return map[string]any{"result": map[string]any{"type": "ok"}}
	})
	watcher := observer{host: host, agents: make(map[string]string)}
	for attempt := 0; attempt < 2; attempt++ {
		if err := watcher.report(context.Background(), "w1:p1", command, sandbox.Info{}, "env"); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(calls, ",") != "pane.process_info,pane.report_agent,pane.report_metadata" {
		t.Fatalf("unexpected requests: %v", calls)
	}
}

func TestUnchangedShellSkipsReports(t *testing.T) {
	host := fakeHerdr(t, func(request rpcRequest) map[string]any {
		t.Errorf("unchanged shell made unexpected request %s", request.Method)
		return map[string]any{"result": map[string]any{"type": "ok"}}
	})
	command := invocation{Mode: "exec", PID: 10, Name: "demo", Agent: "shell"}
	watcher := observer{host: host, agents: map[string]string{"w1:p1": "shell"}, reports: map[string]reportState{
		"w1:p1": {command: command, agent: "shell", state: "unknown", sandbox: "demo", metadata: time.Now(), reported: true},
	}}
	if err := watcher.report(context.Background(), "w1:p1", command, sandbox.Info{ID: "uuid", Name: "demo"}, "shell"); err != nil {
		t.Fatal(err)
	}
}

type countingInventory struct{ calls int }

func (transport *countingInventory) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
}

func TestInventoryCacheRefresh(t *testing.T) {
	transport := &countingInventory{}
	cache := inventoryCache{client: &sandbox.Client{HTTP: &http.Client{Transport: transport}}}
	for _, force := range []bool{false, false, true} {
		if _, err := cache.get(context.Background(), time.Now(), force); err != nil {
			t.Fatal(err)
		}
	}
	if transport.calls != 2 {
		t.Fatalf("got %d inventory reads, want 2", transport.calls)
	}
	if _, err := cache.get(context.Background(), cache.next.Add(time.Second), false); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 3 {
		t.Fatal("expired inventory was not refreshed")
	}
}
