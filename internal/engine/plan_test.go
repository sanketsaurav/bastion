package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
)

// freshFactLines describes a clean Ubuntu 24.04 box with nothing installed.
func freshFactLines() []string {
	return []string{
		"@f os ubuntu 24.04 x86_64",
		"@f sudo ok",
		"@f pkg git absent",
		"@f pkg jq absent",
		"@f file " + b64s("~/.tmux.conf") + " absent",
		"@f bak " + b64s("~/.tmux.conf") + " absent",
		"@f feat docker absent",
		"@f feat tmux absent",
		"@f lcheck mytool needs",
		"@f ualias absent 1000",
		"@f docker absent",
		"@f network absent",
		"@f svc db absent",
		"@f svc web absent",
		"@f sec web absent",
		"@f dvol data absent",
		"@f evol scratch absent",
		"@f end ok",
	}
}

func mustParseFacts(t *testing.T, lines []string) *Facts {
	t.Helper()
	facts, err := ParseFacts(lines)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func actionIDs(plan *Plan) []string {
	ids := make([]string, len(plan.Actions))
	for i, a := range plan.Actions {
		ids[i] = a.ID
	}
	return ids
}

func TestPlanFreshBox(t *testing.T) {
	in := fixtureInput(t)
	plan, err := BuildPlan(in, mustParseFacts(t, freshFactLines()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"bootstrap",
		"packages",
		"feature:docker",
		"feature:tmux",
		"local-feature:mytool",
		"file:~/.tmux.conf",
		"file:~/.config/bastion/shell.sh",
		"shell-line",
		"user-alias",
		"file:~/.hushlogin",
		"network",
		"volume:data",
		"volume:scratch",
		"service:db",
		"health:db",
		"secret:web",
		"service:web",
	}
	got := actionIDs(plan)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("fresh plan order:\n got %v\nwant %v", got, want)
	}
	if plan.HasDestructive() {
		t.Error("a fresh plan must not be destructive")
	}
	if !plan.RootNeeded() {
		t.Error("fresh plan needs root")
	}
}

// convergedFactLines builds facts that exactly match the fixture definition.
func convergedFactLines(t *testing.T, in *Input) []string {
	t.Helper()
	dbCompose, dbDigest, err := GenCompose(in, "db")
	if err != nil || len(dbCompose) == 0 {
		t.Fatal(err)
	}
	_, webDigest, err := GenCompose(in, "web")
	if err != nil {
		t.Fatal(err)
	}
	tmuxSHA := sha256hex([]byte(fixtureTmuxConf))
	shellSHA := sha256hex(shellContent("dev"))
	hushSHA := sha256hex(nil)
	_, sourceDigest, err := PackLocalFeature(in.Dir + "/features/mytool")
	if err != nil {
		t.Fatal(err)
	}
	_, inputsDigest := localFeatureInputs(map[string]any{"channel": "stable"})

	marker := func(v any) string {
		data := mustJSON(t, v)
		return b64s(data)
	}
	return []string{
		"@f os ubuntu 24.04 aarch64",
		"@f sudo ok",
		"@f pkg git installed",
		"@f pkg jq installed",
		"@f file " + b64s("~/.tmux.conf") + " present " + tmuxSHA + " 600",
		"@f bak " + b64s("~/.tmux.conf") + " absent",
		"@f file " + b64s(ShellTarget) + " present " + shellSHA + " 644",
		"@f bak " + b64s(ShellTarget) + " absent",
		"@f file " + b64s(HushloginTarget) + " present " + hushSHA + " 644",
		"@f bak " + b64s(HushloginTarget) + " absent",
		"@f shline present",
		"@f ualias 1000 1000",
		"@f feat docker " + b64s("Docker version 27.0.0"),
		"@f feat tmux " + b64s("tmux 3.4"),
		"@f lcheck mytool ok",
		"@f marker file x " + marker(FileMarker{Target: "~/.tmux.conf", SHA256: tmuxSHA, Mode: "0600"}),
		"@f marker file y " + marker(FileMarker{Target: ShellTarget, SHA256: shellSHA, Mode: "0644"}),
		"@f marker file z " + marker(FileMarker{Target: HushloginTarget, SHA256: hushSHA, Mode: "0644"}),
		"@f marker feature docker " + marker(FeatureMarker{Name: "docker", Version: "1", OptionsDigest: optionsDigest(nil)}),
		"@f marker feature tmux " + marker(FeatureMarker{Name: "tmux", Version: "1", OptionsDigest: optionsDigest(nil)}),
		"@f marker lfeature mytool " + marker(LocalFeatureMarker{Name: "mytool", Version: "2", SourceDigest: sourceDigest, InputsDigest: inputsDigest}),
		"@f docker present " + b64s("Docker version 27.0.0") + " " + b64s("2.29.0"),
		"@f network present",
		"@f svc db present running healthy " + dbDigest + " " + b64s("docker.io/library/postgres:16.4"),
		"@f svc web present running none " + webDigest + " " + b64s("ghcr.io/example/web@sha256:0011"),
		"@f sec web present",
		"@f dvol data present",
		"@f evol scratch present",
		"@f end ok",
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Secret values never enter digests, so a rotated value is invisible to a
// normal plan — RotateSecrets is the explicit gesture: rewrite the env file
// and replace exactly the services that reference secrets.
func TestPlanRotateSecrets(t *testing.T) {
	in := fixtureInput(t)
	in.RotateSecrets = true
	plan, err := BuildPlan(in, mustParseFacts(t, convergedFactLines(t, in)))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(actionIDs(plan), "|")
	if !strings.Contains(got, "secret:web") || !strings.Contains(got, "service:web") {
		t.Errorf("rotation must rewrite and redeploy the secreted service, got %v", actionIDs(plan))
	}
	if strings.Contains(got, "service:db") {
		t.Errorf("services without secretRefs must not be touched, got %v", actionIDs(plan))
	}
	for _, act := range plan.Actions {
		if act.ID == "service:web" {
			if !strings.Contains(act.Summary, "secrets rotated") || !act.service.ForceRecreate {
				t.Errorf("rotation must force-recreate with its reason, got %+v", act.Summary)
			}
		}
	}
}

// A prompt name that already belongs to a different local user must stop
// the plan — aliasing it would hand its identity to someone else's uid.
func TestPlanUserAliasCollision(t *testing.T) {
	in := fixtureInput(t)
	lines := convergedFactLines(t, in)
	for i, l := range lines {
		if strings.HasPrefix(l, "@f ualias ") {
			lines[i] = "@f ualias 4242 1000"
		}
	}
	_, err := BuildPlan(in, mustParseFacts(t, lines))
	if err == nil || !strings.Contains(err.Error(), "different user") {
		t.Fatalf("expected a collision error, got: %v", err)
	}
}

func TestPlanConvergedBoxIsEmpty(t *testing.T) {
	in := fixtureInput(t)
	plan, err := BuildPlan(in, mustParseFacts(t, convergedFactLines(t, in)))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changes() {
		t.Errorf("converged box must produce an empty plan, got %v", actionIDs(plan))
	}
}

func TestPlanDrift(t *testing.T) {
	in := fixtureInput(t)

	t.Run("file content drift", func(t *testing.T) {
		lines := convergedFactLines(t, in)
		for i, l := range lines {
			if strings.HasPrefix(l, "@f file "+b64s("~/.tmux.conf")+" ") {
				lines[i] = "@f file " + b64s("~/.tmux.conf") + " present deadbeef 600"
			}
		}
		plan, err := BuildPlan(in, mustParseFacts(t, lines))
		if err != nil {
			t.Fatal(err)
		}
		if got := actionIDs(plan); strings.Join(got, "|") != "bootstrap|file:~/.tmux.conf" {
			t.Errorf("drift plan = %v", got)
		}
	})

	t.Run("service config drift", func(t *testing.T) {
		lines := convergedFactLines(t, in)
		for i, l := range lines {
			if strings.HasPrefix(l, "@f svc web ") {
				lines[i] = "@f svc web present running none olddigest " + b64s("ghcr.io/example/web:0.9")
			}
		}
		plan, err := BuildPlan(in, mustParseFacts(t, lines))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(actionIDs(plan), "|")
		if got != "bootstrap|secret:web|service:web" {
			t.Errorf("drift plan = %v", got)
		}
	})

	t.Run("stopped service restarts", func(t *testing.T) {
		lines := convergedFactLines(t, in)
		dbCompose, dbDigest, _ := GenCompose(in, "db")
		_ = dbCompose
		for i, l := range lines {
			if strings.HasPrefix(l, "@f svc db ") {
				lines[i] = "@f svc db present exited none " + dbDigest + " " + b64s("postgres")
			}
		}
		plan, err := BuildPlan(in, mustParseFacts(t, lines))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(actionIDs(plan), "|")
		if got != "bootstrap|service:db|health:db" {
			t.Errorf("plan = %v", got)
		}
	})

	t.Run("orphan service removal is destructive", func(t *testing.T) {
		lines := append(convergedFactLines(t, in), "@f osvc oldsvc")
		plan, err := BuildPlan(in, mustParseFacts(t, lines))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(actionIDs(plan), "|")
		if got != "bootstrap|service-remove:oldsvc" {
			t.Fatalf("plan = %v", got)
		}
		if !plan.Actions[1].Destructive || !plan.HasDestructive() {
			t.Error("orphan removal must be destructive")
		}
	})

	t.Run("orphan durable volume is a note, never an action", func(t *testing.T) {
		lines := append(convergedFactLines(t, in), "@f odvol old-data")
		plan, err := BuildPlan(in, mustParseFacts(t, lines))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Changes() {
			t.Errorf("orphan durable data must not create actions, got %v", actionIDs(plan))
		}
		if len(plan.Notes) == 0 || !strings.Contains(plan.Notes[0], "volume delete") {
			t.Errorf("expected an orphan note pointing at volume delete, got %v", plan.Notes)
		}
	})
}

func TestPlanTemplateFile(t *testing.T) {
	in := fixtureInput(t)
	if err := os.WriteFile(filepath.Join(in.Dir, "files", "motd.tmpl"),
		[]byte("box {{ .Box.Name }} on {{ .Provider.Instance }}, data at {{ .Workspace.DataRoot }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in.Box.Host.Files = append(in.Box.Host.Files, config.ManagedFile{
		Source: "files/motd.tmpl", Target: "~/.motd", Mode: "template",
	})
	plan, err := BuildPlan(in, mustParseFacts(t, freshFactLines()))
	if err != nil {
		t.Fatal(err)
	}
	var found *Action
	for i := range plan.Actions {
		if plan.Actions[i].ID == "file:~/.motd" {
			found = &plan.Actions[i]
		}
	}
	if found == nil {
		t.Fatalf("no action for the template file: %v", actionIDs(plan))
	}
	got := string(found.file.Content)
	want := "box testbox on vm, data at /mnt/bastion\n"
	if got != want {
		t.Errorf("rendered content = %q, want %q", got, want)
	}

	t.Run("unknown template key fails at plan time", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(in.Dir, "files", "bad.tmpl"),
			[]byte("{{ .Secrets.token }}"), 0o644); err != nil {
			t.Fatal(err)
		}
		in.Box.Host.Files[len(in.Box.Host.Files)-1] = config.ManagedFile{
			Source: "files/bad.tmpl", Target: "~/.bad", Mode: "template",
		}
		if _, err := BuildPlan(in, mustParseFacts(t, freshFactLines())); err == nil {
			t.Fatal("templates must not have access to undeclared context (like secrets)")
		}
	})
}

func TestPlanRejectsUnsupportedGuest(t *testing.T) {
	in := fixtureInput(t)
	lines := freshFactLines()
	lines[0] = "@f os debian 12 x86_64"
	_, err := BuildPlan(in, mustParseFacts(t, lines))
	if err == nil || !strings.Contains(err.Error(), "Ubuntu 24.04") {
		t.Fatalf("expected unsupported-guest error, got: %v", err)
	}
}

func TestPlanWarnsWithoutSudo(t *testing.T) {
	in := fixtureInput(t)
	lines := freshFactLines()
	lines[1] = "@f sudo missing"
	plan, err := BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "sudo") {
		t.Errorf("expected a sudo warning, got %v", plan.Warnings)
	}
}

func TestPlanTruncatedInspectionFails(t *testing.T) {
	in := fixtureInput(t)
	lines := freshFactLines()
	_, err := BuildPlan(in, mustParseFacts(t, lines[:len(lines)-1]))
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("expected truncation error, got: %v", err)
	}
}

func TestPlanGuestUnknown(t *testing.T) {
	in := fixtureInput(t)
	plan, err := BuildPlan(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.GuestUnknown || plan.Changes() {
		t.Errorf("nil facts must yield a guest-unknown plan, got %+v", plan)
	}
}
