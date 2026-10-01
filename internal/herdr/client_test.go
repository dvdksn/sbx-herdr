package herdr

import "testing"

func TestClassification(t *testing.T) {
	for _, test := range []struct {
		data, state string
	}{
		{`{"state":"idle","matched_rule":{"id":"welcome_prompt_footer","priority":120,"state":"idle"}}`, "idle"},
		{`{"state":"working","matched_rule":"spinner"}`, "working"},
		{`{"state":"idle","matched_rule":null}`, "unknown"},
		{`{"state":"idle"}`, "unknown"},
		{`{"state":"blocked","matched_rule":{"id":"approval"},"skip_state_update":true}`, "unknown"},
		{`{"state":"done","matched_rule":{"id":"done"}}`, "unknown"},
	} {
		t.Run(test.data, func(t *testing.T) {
			state, err := Classification([]byte(test.data))
			if err != nil || state != test.state {
				t.Fatalf("got %q, %v", state, err)
			}
		})
	}
}
