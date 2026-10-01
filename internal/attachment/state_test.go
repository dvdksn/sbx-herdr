package attachment

import "testing"

func TestAgent(t *testing.T) {
	for _, test := range []struct{ command, agent string }{
		{"codex", "codex"},
		{"/usr/local/bin/claude", "claude"},
		{"sbx-kit-devin", "devin"},
		{"docker.io/docker/sbx-kit-codex:0.155.1", "codex"},
		{"docker.io/docker/sbx-kit-devin@sha256:0", "devin"},
		{"registry.example:5000/team/claude:1.0", "claude"},
		{"sbx-kit-builder", ""},
		{"bash", ""},
	} {
		t.Run(test.command, func(t *testing.T) {
			if got := Agent(test.command); got != test.agent {
				t.Fatalf("got %q", got)
			}
		})
	}
}
