package engine

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// Generated programs must always be syntactically valid bash — the closest we
// can get to executing them without a guest.
func TestGeneratedScriptsAreValidBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	in := fixtureInput(t)
	scripts := map[string][]byte{
		"inspect": GenInspectScript(in),
	}
	plan := freshPlan(t, in)
	apply, err := GenApplyScript(in, plan, ApplyOptions{SecretValues: map[string]string{"token": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	scripts["apply"] = apply

	// A destructive plan exercises the removal bodies too.
	lines := append(convergedFactLines(t, in), "@f osvc oldsvc")
	removalPlan, err := BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	removal, err := GenApplyScript(in, removalPlan, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scripts["removal"] = removal

	for name, script := range scripts {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = bytes.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s script fails bash -n: %v\n%s", name, err, out)
		}
	}
}

// The run harness must fail a step when any command in it fails — pipeline
// stages included — and stop before later steps. Without this, a failed
// installer pipe followed by the unconditional marker write reported ok and
// recorded a marker for a feature that was never installed.
func TestRunHarnessStepFailureSemantics(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	run := func(steps string) (string, int) {
		t.Helper()
		w := &scriptWriter{}
		w.header()
		w.runFn()
		w.raw(steps)
		cmd := exec.Command(bash)
		cmd.Stdin = bytes.NewReader(w.bytes())
		out, err := cmd.Output()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}

	out, code := run(`s0() {
  sh -c 'exit 3' | cat
  echo marker-written
}
s1() { true; }
run a0 s0
run a1 s1
printf '@e apply done\n'
`)
	if code != 20 || !strings.Contains(out, "@e a0 fail") || strings.Contains(out, "@e a0 ok") ||
		strings.Contains(out, "@e a1 start") || strings.Contains(out, "apply done") {
		t.Errorf("mid-step pipeline failure must fail the step and stop the run; exit=%d output:\n%s", code, out)
	}

	out, code = run(`s0() {
  false || true
  missing-cmd-for-test 2>/dev/null || echo fallback
}
run a0 s0
printf '@e apply done\n'
`)
	if code != 0 || !strings.Contains(out, "@e a0 ok") || !strings.Contains(out, "@e apply done") {
		t.Errorf("guarded failures must not fail the step; exit=%d output:\n%s", code, out)
	}
}
