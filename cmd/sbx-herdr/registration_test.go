package main

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

type registrationStep struct {
	binary string
	args   []string
	output string
	err    error
}

func scriptedRegistration(t *testing.T, steps []registrationStep) registration {
	t.Helper()
	index := 0
	t.Cleanup(func() {
		if index != len(steps) {
			t.Errorf("ran %d of %d commands", index, len(steps))
		}
	})
	return registration{out: io.Discard, herdr: "herdr", sbx: "sbx", command: func(_ bool, binary string, args ...string) ([]byte, error) {
		t.Helper()
		if index >= len(steps) {
			t.Fatalf("unexpected command %s %q", binary, args)
		}
		step := steps[index]
		index++
		if binary != step.binary || !reflect.DeepEqual(args, step.args) {
			t.Fatalf("command %d: got %s %q, want %s %q", index, binary, args, step.binary, step.args)
		}
		return []byte(step.output), step.err
	}}
}
func step(binary, output string, args ...string) registrationStep {
	return registrationStep{binary: binary, args: args, output: output}
}

const profile = `[{"id":"m1","target":"demo.sbx","session":"default","enabled":true}]`

var demo = sandbox.Info{Name: "demo", ID: "s1", Status: "running"}

func inventory(items ...sandbox.Info) func() ([]sandbox.Info, error) {
	return func() ([]sandbox.Info, error) { return items, nil }
}

func TestRegistrationAddCreatesNativeMachine(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", "[]", "machine", "list", "--json"),
		step("herdr", "", "machine", "add", "demo.sbx", "--label", "demo", "--remote-session", "default"),
		step("herdr", profile, "machine", "list", "--json"),
	})
	r.inventory = inventory(demo)
	if err := r.add(demo); err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationAddReusesDisabledMachine(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", strings.Replace(profile, `true`, `false`, 1), "machine", "list", "--json"),
		step("herdr", "", "machine", "enable", "m1"),
	})
	r.inventory = inventory(demo)
	if err := r.add(demo); err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationRejectsReplacedSandbox(t *testing.T) {
	r := scriptedRegistration(t, nil)
	replacement := demo
	replacement.ID = "s2"
	r.inventory = inventory(replacement)
	if err := r.add(demo); err == nil {
		t.Fatal("accepted replacement sandbox")
	}
}
func TestRegistrationStartsStoppedSandbox(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("sbx", "", "exec", "demo", "true"),
		step("herdr", profile, "machine", "list", "--json"),
	})
	stopped := demo
	stopped.Status = "stopped"
	r.inventory = inventory(stopped)
	if err := r.add(stopped); err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationRemoveStaleMachineWithoutSBX(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", profile, "machine", "list", "--json"),
		step("herdr", "", "machine", "remove", "m1"),
	})
	r.inventory = func() ([]sandbox.Info, error) { t.Fatal("removal queried SBX"); return nil, nil }
	if err := r.remove(savedMachine{ID: "m1", Target: "demo.sbx", Session: "default"}); err != nil {
		t.Fatal(err)
	}
}
func TestRegistrationRemoveRefusesChangedTarget(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", strings.Replace(profile, "demo.sbx", "other.sbx", 1), "machine", "list", "--json"),
	})
	if err := r.remove(savedMachine{ID: "m1", Target: "demo.sbx", Session: "default"}); err == nil {
		t.Fatal("removed changed profile")
	}
}
func TestRegistrationDuplicateProfiles(t *testing.T) {
	var machines []savedMachine
	if err := json.Unmarshal([]byte(profile), &machines); err != nil {
		t.Fatal(err)
	}
	machines = append(machines, machines[0])
	if _, err := matchingRegistration(machines, "demo"); err == nil {
		t.Fatal("accepted duplicate")
	}
}
func TestRegistrationInstallFailureStopsSetup(t *testing.T) {
	failure := errors.New("installation declined")
	failed := step("herdr", "", "machine", "add", "demo.sbx", "--label", "demo", "--remote-session", "default")
	failed.err = failure
	r := scriptedRegistration(t, []registrationStep{step("herdr", "[]", "machine", "list", "--json"), failed})
	r.inventory = inventory(demo)
	if err := r.add(demo); !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
}
func TestRegistrationAddReusesEnabledMachine(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", profile, "machine", "list", "--json"),
	})
	r.inventory = inventory(demo)
	if err := r.add(demo); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationRemoveNonDefaultSession(t *testing.T) {
	machines := strings.Replace(profile, "default", "agents", 1)
	r := scriptedRegistration(t, []registrationStep{
		step("herdr", machines, "machine", "list", "--json"),
		step("herdr", "", "machine", "remove", "m1"),
	})
	if err := r.remove(savedMachine{ID: "m1", Target: "demo.sbx", Session: "agents"}); err != nil {
		t.Fatal(err)
	}
}
