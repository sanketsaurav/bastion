package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalDoc = `
apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: dev
provider:
  name: gcp
  project: proj
  zone: us-west1-a
  instance: vm
`

func parseValid(t *testing.T, doc string) *Box {
	t.Helper()
	box, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return box
}

func validateDoc(t *testing.T, doc string) []string {
	t.Helper()
	return ValidateBox(parseValid(t, doc), "")
}

func wantIssue(t *testing.T, issues []string, substr string) {
	t.Helper()
	for _, issue := range issues {
		if strings.Contains(issue, substr) {
			return
		}
	}
	t.Fatalf("no issue containing %q in %v", substr, issues)
}

func TestParseMinimalAndDefaults(t *testing.T) {
	box := parseValid(t, minimalDoc)
	if issues := ValidateBox(box, ""); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if box.Provider.Mode != "attached" {
		t.Errorf("provider.mode default = %q, want attached", box.Provider.Mode)
	}
	if box.Connection.Type != "iap" {
		t.Errorf("connection.type default = %q, want iap", box.Connection.Type)
	}
	if !box.Connection.UseOSLogin() {
		t.Error("osLogin should default to true")
	}
	if box.Runtime.Engine != "docker" {
		t.Errorf("runtime.engine default = %q, want docker", box.Runtime.Engine)
	}
	if box.Workspace.Mount != "/workspace" || box.Workspace.DataRoot != "/mnt/bastion" {
		t.Errorf("workspace defaults = %+v", box.Workspace)
	}
}

func TestServiceDefaults(t *testing.T) {
	box := parseValid(t, minimalDoc+`
services:
  web:
    image: example/web:1
    endpoints:
      http:
        containerPort: 8080
`)
	svc := box.Services["web"]
	if svc.PullPolicy != "if-not-present" {
		t.Errorf("pullPolicy default = %q", svc.PullPolicy)
	}
	if svc.RestartPolicy != "unless-stopped" {
		t.Errorf("restartPolicy default = %q, want unless-stopped (SPEC Δ1)", svc.RestartPolicy)
	}
	if !svc.IsEnabled() {
		t.Error("enabled should default to true")
	}
	ep := svc.Endpoints["http"]
	if ep.Protocol != "http" || ep.Visibility != "internal" {
		t.Errorf("endpoint defaults = %+v", ep)
	}
}

func TestUnknownFieldsAreErrors(t *testing.T) {
	cases := map[string]string{
		"top level":     minimalDoc + "\nbogusField: 1\n",
		"nested":        strings.Replace(minimalDoc, "instance: vm", "instance: vm\n  regiom: oops", 1),
		"service level": minimalDoc + "\nservices:\n  web:\n    image: x\n    imagePullPolicy: always\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatal("expected an unknown-field error")
			} else if !strings.Contains(err.Error(), "field") {
				t.Fatalf("error should name the unknown field, got: %v", err)
			}
		})
	}
}

func TestEmptyDocument(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Fatal("expected an error for an empty document")
	}
}

func TestDocumentHeaderValidation(t *testing.T) {
	issues := validateDoc(t, strings.Replace(minimalDoc, "bastion/v1alpha1", "bastion/v9", 1))
	wantIssue(t, issues, "apiVersion")

	issues = validateDoc(t, strings.Replace(minimalDoc, "kind: Box", "kind: Machine", 1))
	wantIssue(t, issues, "kind")

	issues = validateDoc(t, strings.Replace(minimalDoc, "name: dev", "name: Not_A_Label", 1))
	wantIssue(t, issues, "DNS label")
}

func TestReservedValues(t *testing.T) {
	t.Run("managed mode", func(t *testing.T) {
		doc := strings.Replace(minimalDoc, "name: gcp", "name: gcp\n  mode: managed", 1)
		wantIssue(t, validateDoc(t, doc), "reserved for a later milestone")
	})
	t.Run("public visibility", func(t *testing.T) {
		doc := minimalDoc + `
services:
  web:
    image: x
    endpoints:
      http:
        containerPort: 80
        visibility: public
`
		wantIssue(t, validateDoc(t, doc), "reserved for a later milestone")
	})
	t.Run("template file mode is accepted", func(t *testing.T) {
		doc := minimalDoc + `
host:
  files:
    - source: files/motd
      target: /etc/motd
      mode: template
`
		if issues := validateDoc(t, doc); len(issues) != 0 {
			t.Fatalf("template mode must validate, got: %v", issues)
		}
	})
	t.Run("bogus file mode", func(t *testing.T) {
		doc := minimalDoc + `
host:
  files:
    - source: files/motd
      target: /etc/motd
      mode: overwrite
`
		wantIssue(t, validateDoc(t, doc), `"replace" or "template"`)
	})
}

func TestConnectionValidation(t *testing.T) {
	doc := minimalDoc + `
connection:
  type: direct
`
	issues := validateDoc(t, doc)
	wantIssue(t, issues, "connection.host")
	wantIssue(t, issues, "connection.user")

	doc = minimalDoc + `
connection:
  type: iap
  host: example.com
`
	wantIssue(t, validateDoc(t, doc), "only to type \"direct\"")
}

func TestEnvValueForms(t *testing.T) {
	box := parseValid(t, minimalDoc+`
secrets:
  token:
    source:
      environment: TOKEN
services:
  web:
    image: x
    environment:
      PLAIN: hello
      NUM: 42
      SECRET:
        secretRef: token
`)
	env := box.Services["web"].Environment
	if v := env["PLAIN"]; !v.IsLiteral || v.Literal != "hello" {
		t.Errorf("PLAIN = %+v", v)
	}
	if v := env["NUM"]; !v.IsLiteral || v.Literal != "42" {
		t.Errorf("NUM = %+v", v)
	}
	if v := env["SECRET"]; v.IsLiteral || v.SecretRef != "token" {
		t.Errorf("SECRET = %+v", v)
	}
}

func TestEnvValueRejectsUnknownKeys(t *testing.T) {
	_, err := Parse([]byte(minimalDoc + `
services:
  web:
    image: x
    environment:
      BAD:
        secretName: nope
`))
	if err == nil || !strings.Contains(err.Error(), "secretRef") {
		t.Fatalf("expected a secretRef-only error, got: %v", err)
	}
}

func TestServiceValidation(t *testing.T) {
	t.Run("missing image", func(t *testing.T) {
		wantIssue(t, validateDoc(t, minimalDoc+"\nservices:\n  web: {}\n"), "image is required")
	})
	t.Run("undeclared secret", func(t *testing.T) {
		doc := minimalDoc + `
services:
  web:
    image: x
    environment:
      TOKEN:
        secretRef: ghost
`
		wantIssue(t, validateDoc(t, doc), "not a declared secret")
	})
	t.Run("undeclared volume", func(t *testing.T) {
		doc := minimalDoc + `
services:
  web:
    image: x
    mounts:
      - volume: ghost
        target: /data
`
		wantIssue(t, validateDoc(t, doc), "not a declared volume")
	})
	t.Run("mount volume xor source", func(t *testing.T) {
		doc := minimalDoc + `
volumes:
  data:
    persistence: durable
services:
  web:
    image: x
    mounts:
      - volume: data
        source: /srv/config
        target: /data
`
		wantIssue(t, validateDoc(t, doc), "not both")
	})
	t.Run("relative bind source", func(t *testing.T) {
		doc := minimalDoc + `
services:
  web:
    image: x
    mounts:
      - source: relative/path
        target: /data
`
		wantIssue(t, validateDoc(t, doc), "absolute remote path")
	})
	t.Run("dependency cycle", func(t *testing.T) {
		doc := minimalDoc + `
services:
  a:
    image: x
    dependsOn: [b]
  b:
    image: x
    dependsOn: [c]
  c:
    image: x
    dependsOn: [a]
`
		wantIssue(t, validateDoc(t, doc), "dependency cycle")
	})
	t.Run("self dependency", func(t *testing.T) {
		doc := minimalDoc + `
services:
  a:
    image: x
    dependsOn: [a]
`
		wantIssue(t, validateDoc(t, doc), "cannot depend on itself")
	})
	t.Run("unknown dependency", func(t *testing.T) {
		doc := minimalDoc + `
services:
  a:
    image: x
    dependsOn: [ghost]
`
		wantIssue(t, validateDoc(t, doc), "not a declared service")
	})
	t.Run("invalid port", func(t *testing.T) {
		doc := minimalDoc + `
services:
  web:
    image: x
    endpoints:
      http:
        containerPort: 70000
`
		wantIssue(t, validateDoc(t, doc), "not a valid port")
	})
}

func TestSecretValidation(t *testing.T) {
	doc := minimalDoc + `
secrets:
  both:
    source:
      environment: A
      file: ~/.secret
  neither:
    source: {}
`
	issues := validateDoc(t, doc)
	wantIssue(t, issues, "not both")
	wantIssue(t, issues, "exactly one")
}

func TestShellValidation(t *testing.T) {
	// The prompt lands inside a single-quoted PS1 assignment: quotes,
	// spaces, and backslashes must be impossible, not merely escaped.
	wantIssue(t, validateDoc(t, minimalDoc+`
host:
  shell:
    prompt: "sanket's"
`), "host.shell.prompt")
	wantIssue(t, validateDoc(t, minimalDoc+`
host:
  shell: {}
`), "prompt is required")
	if issues := validateDoc(t, minimalDoc+`
host:
  shell:
    prompt: sanket
`); len(issues) != 0 {
		t.Fatalf("valid prompt rejected: %v", issues)
	}
}

func TestManagedFileValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "files", "ok.conf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	box := parseValid(t, minimalDoc+`
host:
  files:
    - source: files/ok.conf
      target: ~/.ok.conf
    - source: files/missing.conf
      target: ~/.missing.conf
    - source: ../escape.conf
      target: /etc/escape
    - source: files/ok.conf
      target: relative/target
      permissions: "9999"
`)
	issues := ValidateBox(box, dir)
	wantIssue(t, issues, "files/missing.conf")
	wantIssue(t, issues, "stay inside the box directory")
	wantIssue(t, issues, "absolute or start with ~/")
	wantIssue(t, issues, "octal")
}

func TestFeatureValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "features", "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	box := parseValid(t, minimalDoc+`
host:
  features:
    - uses: docker
    - uses: ./features/mine
    - uses: ./features/ghost
    - uses: not-a-real-feature
    - uses: ../outside
`)
	issues := ValidateBox(box, dir)
	wantIssue(t, issues, "./features/ghost")
	wantIssue(t, issues, "unknown built-in feature")
	wantIssue(t, issues, "stay inside the box directory")
	for _, issue := range issues {
		if strings.Contains(issue, "features/mine") || strings.Contains(issue, `"docker"`) {
			t.Errorf("unexpected issue for a valid feature: %s", issue)
		}
	}
}

func TestByteSizeParsing(t *testing.T) {
	cases := map[string]int64{
		"10MiB":  10 << 20,
		"1GiB":   1 << 30,
		"512KiB": 512 << 10,
		"100MB":  100_000_000,
		"1.5GiB": 3 << 29,
		"42":     42,
		"7B":     7,
	}
	for in, want := range cases {
		got, err := ParseByteSize(in)
		if err != nil {
			t.Errorf("ParseByteSize(%q): %v", in, err)
			continue
		}
		if got.Bytes() != want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", in, got.Bytes(), want)
		}
	}
	for _, bad := range []string{"", "MiB", "10 parsecs", "-5MiB"} {
		if _, err := ParseByteSize(bad); err == nil {
			t.Errorf("ParseByteSize(%q): expected an error", bad)
		}
	}
}

func TestDurationParsing(t *testing.T) {
	if _, err := Parse([]byte(minimalDoc + `
services:
  web:
    image: x
    healthcheck:
      command: ["ok"]
      interval: 30q
`)); err == nil || !strings.Contains(err.Error(), "duration") {
		t.Fatalf("expected a duration error, got: %v", err)
	}
}

func TestLoadExample(t *testing.T) {
	loaded, err := Load(filepath.Join("..", "..", "examples", "agents"))
	if err != nil {
		t.Fatalf("example must validate: %v", err)
	}
	if loaded.Box.Metadata.Name != "agents" {
		t.Errorf("example name = %q", loaded.Box.Metadata.Name)
	}
	if len(loaded.Box.Services) != 2 {
		t.Errorf("example services = %d, want 2", len(loaded.Box.Services))
	}
}

func TestValidationErrorAggregates(t *testing.T) {
	dir := t.TempDir()
	doc := strings.Replace(minimalDoc, "name: dev", "name: Bad_Name", 1) + `
services:
  web: {}
`
	if err := os.WriteFile(filepath.Join(dir, "bastion.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	verr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if len(verr.Issues) < 2 {
		t.Errorf("expected multiple aggregated issues, got: %v", verr.Issues)
	}
}
