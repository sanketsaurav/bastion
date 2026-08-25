package engine

import (
	"strings"
	"testing"
)

// Undeclared feature markers must surface as plan notes — never as actions:
// apply stays additive, and cleanup is the user's explicit call.
func TestPlanReportsOrphanFeatureMarkers(t *testing.T) {
	in := fixtureInput(t)
	lines := append(convergedFactLines(t, in),
		"@f fmark docker",      // declared → silent
		"@f fmark bun",         // user-level → feature remove note
		"@f fmark github-cli",  // apt-based → manual removal note
		"@f fmark weird-thing", // unknown to this version
		"@f lmark mytool",      // declared local → silent
		"@f lmark oldtool",     // undeclared local
	)
	plan, err := BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("orphan markers must never produce actions, got %d", len(plan.Actions))
	}
	var orphan []string
	for _, n := range plan.Notes {
		if strings.Contains(n, "no longer declared") || strings.Contains(n, "unknown to this version") {
			orphan = append(orphan, n)
		}
	}
	if len(orphan) != 4 {
		t.Fatalf("expected 4 orphan notes, got %d: %v", len(orphan), orphan)
	}
	joined := strings.Join(orphan, "\n")
	for _, want := range []string{
		"bastion feature remove " + in.BoxID + " bun",
		"apt-get remove gh",
		`"weird-thing"`,
		`local feature "oldtool"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"docker"`) || strings.Contains(joined, `"mytool"`) {
		t.Errorf("declared features must not be reported as orphans:\n%s", joined)
	}
}

func TestFeatureRemoveScript(t *testing.T) {
	in := fixtureInput(t)
	def := Builtins["claude-code"]
	script := FeatureRemoveScript(in, def)
	for _, want := range []string{
		`"$HOME"/.local/bin/claude`,
		`"$HOME"/.local/share/claude`,
		"rm -rf --",
		"sudo -n rm -f -- /var/lib/bastion/state/" + in.BoxID + "/features/claude-code.json",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
	// Removal must never reach into user configuration or credentials.
	if strings.Contains(script, ".claude.json") || strings.Contains(script, `"$HOME"/.claude `) {
		t.Errorf("script must not touch ~/.claude data:\n%s", script)
	}
}

// Plan decides whether a builtin is installed by looking up the fact keyed
// by def.Name, so every check must report under the feature's own name. A
// check keyed by the binary instead (claude for claude-code) makes the
// feature replan as "not installed" forever after a successful apply.
func TestBuiltinChecksReportUnderFeatureName(t *testing.T) {
	for key, def := range Builtins {
		if key != def.Name {
			t.Errorf("builtin registered under %q has Name %q", key, def.Name)
		}
		if !strings.Contains(def.CheckBash, "f feat "+q(def.Name)+" ") {
			t.Errorf("feature %q: CheckBash never reports fact %q:\n%s", key, def.Name, def.CheckBash)
			continue
		}
		if !strings.Contains(def.CheckBash, "f feat "+q(def.Name)+" absent") {
			t.Errorf("feature %q: CheckBash has no absent branch for fact %q:\n%s", key, def.Name, def.CheckBash)
		}
	}
}
