package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func freshPlan(t *testing.T, in *Input) *Plan {
	t.Helper()
	plan, err := BuildPlan(in, mustParseFacts(t, freshFactLines()))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestGenApplyScriptGolden(t *testing.T) {
	in := fixtureInput(t)
	plan := freshPlan(t, in)
	script, err := GenApplyScript(in, plan, ApplyOptions{SecretValues: map[string]string{"token": "sekrit-value"}})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "apply-fresh.sh", script)
}

func TestApplyScriptInvariants(t *testing.T) {
	in := fixtureInput(t)
	plan := freshPlan(t, in)
	script, err := GenApplyScript(in, plan, ApplyOptions{SecretValues: map[string]string{"token": "sekrit-value"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)

	// Secret values are embedded base64 (transits SSH stdin only) — never as
	// plaintext, and never on any command line.
	if strings.Contains(s, "sekrit-value") {
		t.Error("secret value must not appear in plaintext in the apply script")
	}
	if !strings.Contains(s, b64([]byte("API_TOKEN=sekrit-value\n"))) {
		t.Error("secret env file content missing (base64)")
	}
	// Remote lock protects concurrent applies.
	if !strings.Contains(s, "LOCK") || !strings.Contains(s, "exit 21") {
		t.Error("remote lock preamble missing")
	}
	// Positional step ids drive the event protocol.
	if !strings.Contains(s, "run a0 s0") {
		t.Error("positional step invocation missing")
	}
	// Privileged operations go through sudo -n (never interactive sudo).
	if strings.Contains(strings.ReplaceAll(s, "sudo -n", ""), "sudo ") {
		t.Error("all sudo usage must be non-interactive (sudo -n)")
	}
	// The docker feature exists in this plan, so apt sources appear.
	if !strings.Contains(s, "download.docker.com") {
		t.Error("docker feature body missing")
	}
	// Every apt invocation must wait out dpkg locks (cloud-init races).
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "apt-get") && !strings.Contains(line, "DPkg::Lock::Timeout") {
			t.Errorf("apt invocation without lock timeout: %s", line)
		}
	}
	// Local feature payload is uploaded, never fetched from the network.
	if !strings.Contains(s, "tar -xzf") {
		t.Error("local feature tarball extraction missing")
	}
}

func TestApplyPlanSuccessFlow(t *testing.T) {
	in := fixtureInput(t)
	plan := freshPlan(t, in)
	var lines []string
	for i := range plan.Actions {
		lines = append(lines, "@e a"+itoa(i)+" start", "@e a"+itoa(i)+" ok")
	}
	lines = append(lines, "@e apply done")
	sess := &fakeSession{lines: lines}

	var started, done []string
	result, err := ApplyPlan(context.Background(), sess, in, plan,
		ApplyOptions{SecretValues: map[string]string{"token": "v"}},
		ApplyHooks{
			OnStart: func(id string) { started = append(started, id) },
			OnDone:  func(id, status string) { done = append(done, id+"="+status) },
		})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Done || len(result.Completed) != len(plan.Actions) {
		t.Errorf("result = %+v", result)
	}
	if started[0] != "bootstrap" || !strings.HasPrefix(done[0], "bootstrap=ok") {
		t.Errorf("hooks must receive real action IDs, got start=%v done=%v", started[:1], done[:1])
	}
}

func TestApplyPlanFailureIsResumable(t *testing.T) {
	in := fixtureInput(t)
	plan := freshPlan(t, in)
	sess := &fakeSession{
		lines: []string{
			"@e a0 start", "@e a0 ok",
			"@e a1 start",
			"@l a1 " + b64s("E: Unable to locate package git"),
			"@e a1 fail",
		},
		exit: 20,
	}
	result, err := ApplyPlan(context.Background(), sess, in, plan, ApplyOptions{SecretValues: map[string]string{"token": "v"}}, ApplyHooks{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if result.Failed != "packages" {
		t.Errorf("failed action = %q", result.Failed)
	}
	if len(result.Completed) != 1 || result.Completed[0] != "bootstrap" {
		t.Errorf("completed = %v", result.Completed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "Unable to locate package") || !strings.Contains(msg, "rerun `bastion apply`") {
		t.Errorf("error must carry logs and the resume hint, got: %s", msg)
	}
}

func TestApplyPlanRemoteLockHeld(t *testing.T) {
	in := fixtureInput(t)
	plan := freshPlan(t, in)
	sess := &fakeSession{lines: []string{"@e lock fail"}, exit: 21}
	result, err := ApplyPlan(context.Background(), sess, in, plan, ApplyOptions{SecretValues: map[string]string{"token": "v"}}, ApplyHooks{})
	if err == nil || !strings.Contains(err.Error(), "remote lock") {
		t.Fatalf("expected a lock error, got: %v", err)
	}
	if !result.LockHeld {
		t.Error("LockHeld must be set")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
