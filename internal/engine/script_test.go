package engine

import (
	"bytes"
	"os/exec"
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
