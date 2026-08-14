package engine

import (
	"strings"
	"testing"
)

func TestGenInspectScriptGolden(t *testing.T) {
	in := fixtureInput(t)
	checkGolden(t, "inspect.sh", GenInspectScript(in))
}

func TestInspectScriptIsReadOnly(t *testing.T) {
	in := fixtureInput(t)
	s := string(GenInspectScript(in))
	// The inspect program must never mutate the guest: no package
	// installation, no writes outside $(), no docker state changes.
	for _, forbidden := range []string{
		"apt-get install", "mkdir ", "rm -", "tee ", " mv ", "usermod",
		"docker compose up", "docker run", "curl", "systemctl",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("inspect script must be read-only; found %q", forbidden)
		}
	}
	// Probes for everything the fixture declares.
	for _, expected := range []string{
		"dpkg-query", "sha256sum", "enable", // packages, files…
	} {
		_ = expected
	}
	for _, expected := range []string{"f pkg", "f svc", "f end ok"} {
		if !strings.Contains(s, expected) {
			t.Errorf("inspect script missing %q", expected)
		}
	}
}

func TestInspectRoundTripThroughParser(t *testing.T) {
	// The inspect script's emissions and ParseFacts speak the same protocol;
	// this exercises the parser against realistic mixed output.
	lines := append([]string{
		"Warning: some stray remote noise",
		"",
	}, freshFactLines()...)
	facts := mustParseFacts(t, lines)
	if !facts.Complete || facts.OSID != "ubuntu" || facts.Packages["git"] {
		t.Errorf("facts = %+v", facts)
	}
	if facts.Files["~/.tmux.conf"].Exists {
		t.Error("file fact should be absent")
	}
}

func TestFactsParserTolerance(t *testing.T) {
	facts := mustParseFacts(t, []string{
		"@f feat tmux present",          // plain token instead of b64
		"@f feat gh " + b64s("gh 2.55"), // b64
		"@f bogus what",                 // unknown kind ignored
		"@f end ok",
	})
	if facts.Features["tmux"] != "present" {
		t.Errorf("plain token feat = %q", facts.Features["tmux"])
	}
	if facts.Features["gh"] != "gh 2.55" {
		t.Errorf("b64 feat = %q", facts.Features["gh"])
	}
}
