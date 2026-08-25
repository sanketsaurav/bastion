package engine

import (
	"strings"
	"testing"
)

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
