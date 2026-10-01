package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Info struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Agent     string `json:"agent,omitempty"`
	Status    string `json:"status"`
	Workspace string `json:"workspace"`
}

type Client struct{ HTTP *http.Client }

func New(socket string) *Client {
	return &Client{HTTP: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
		},
		ResponseHeaderTimeout: 5 * time.Second,
	}}}
}

func Discover(ctx context.Context, binary, override string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("SBX_HERDR_SOCKET must be absolute")
		}
		return override, nil
	}
	data, err := exec.CommandContext(ctx, binary, "daemon", "status", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("discover sandboxd socket: %w", err)
	}
	var status struct {
		Socket string `json:"socket"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return "", err
	}
	if !filepath.IsAbs(status.Socket) {
		return "", fmt.Errorf("sbx daemon status returned no absolute socket path")
	}
	return status.Socket, nil
}

func (client *Client) request(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost"+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		var problem struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&problem)
		return nil, fmt.Errorf("sandboxd GET %s: HTTP %d: %s", path, response.StatusCode, problem.Message)
	}
	return response, nil
}

func (client *Client) read(ctx context.Context, path string, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, err := client.request(ctx, path)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result)
}

func (client *Client) Health(ctx context.Context) error {
	var health struct {
		APIVersion string `json:"api_version"`
	}
	if err := client.read(ctx, "/daemon/health", &health); err != nil {
		return err
	}
	parts := strings.Split(health.APIVersion, ".")
	if len(parts) != 3 {
		return fmt.Errorf("sandboxd reports no supported API version: %q", health.APIVersion)
	}
	if parts[0] != "0" {
		return fmt.Errorf("requires sandboxd API major 0; got %s", health.APIVersion)
	}
	return nil
}

func (client *Client) List(ctx context.Context) ([]Info, error) {
	var result []Info
	err := client.read(ctx, "/sandbox", &result)
	return result, err
}
