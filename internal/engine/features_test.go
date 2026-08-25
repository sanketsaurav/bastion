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
		"@f pmark bun unzip",   // covered by bun's own orphan note → silent
		"@f pmark uv curl",     // dangling: no uv marker, not declared
		"@f pmark docker jq",   // declared feature → silent
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
		if strings.Contains(n, "no longer declared") || strings.Contains(n, "unknown to this version") ||
			strings.Contains(n, "prerequisite") {
			orphan = append(orphan, n)
		}
	}
	if len(orphan) != 5 {
		t.Fatalf("expected 5 orphan notes, got %d: %v", len(orphan), orphan)
	}
	joined := strings.Join(orphan, "\n")
	for _, want := range []string{
		"bastion feature remove " + in.BoxID + " bun",
		"apt-get remove gh",
		`"weird-thing"`,
		`local feature "oldtool"`,
		`package "curl" was installed by bastion as a prerequisite of feature "uv"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, `"docker"`) || strings.Contains(joined, `"mytool"`) {
		t.Errorf("declared features must not be reported as orphans:\n%s", joined)
	}
	// bun's prereq is covered by bun's own orphan note; docker's is owned.
	if strings.Contains(joined, `"unzip"`) || strings.Contains(joined, `"jq"`) {
		t.Errorf("owned or feature-covered prerequisites must not get their own notes:\n%s", joined)
	}
}

// Apply must claim only the prerequisites it actually installs: the guard
// checks dpkg first, and the prereq marker write sits inside the install
// branch — a package found present leaves no marker and is never removed.
func TestAptPrereqApplyAndRemove(t *testing.T) {
	in := fixtureInput(t)
	def := Builtins["bun"]
	body, err := featureBody(in, &featureAction{Name: def.Name, Def: def, Version: def.Version})
	if err != nil {
		t.Fatal(err)
	}
	guard := `if [ "$(dpkg-query -W -f='${db:Status-Status}' unzip 2>/dev/null)" = installed ]`
	if !strings.Contains(body, guard) {
		t.Errorf("apply body missing the already-installed guard:\n%s", body)
	}
	markerLine := "/prereqs/bun/.unzip.tmp"
	idx := strings.Index(body, markerLine)
	fi := strings.Index(body, "\nfi\n")
	if idx == -1 || fi == -1 || idx > fi {
		t.Errorf("prereq marker write must sit inside the install branch:\n%s", body)
	}
	if !strings.Contains(body, "install -y unzip") {
		t.Errorf("apply body must install the missing prerequisite:\n%s", body)
	}

	script := FeatureRemoveScript(in, def)
	for _, want := range []string{
		"/prereqs/bun/unzip.json",
		"grep -c '^Remv '",
		"remove -y -qq unzip",
		"kept prerequisite package unzip",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("remove script missing %q:\n%s", want, script)
		}
	}
}

// A feature that may install apt packages needs root to do it.
func TestAptPrereqsRequireRoot(t *testing.T) {
	for name, def := range Builtins {
		if len(def.AptPrereqs) > 0 && !def.RequiresRoot {
			t.Errorf("feature %q declares AptPrereqs but not RequiresRoot", name)
		}
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
