package engine

import (
	"strings"
	"testing"
)

func TestGenComposeGolden(t *testing.T) {
	in := fixtureInput(t)
	for _, svc := range []string{"db", "web"} {
		content, digest, err := GenCompose(in, svc)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), digest) {
			t.Errorf("%s: rendered compose must carry its own digest label", svc)
		}
		checkGolden(t, "compose-"+svc+".yaml", content)
	}
}

func TestGenComposeDeterministic(t *testing.T) {
	in := fixtureInput(t)
	first, digest1, err := GenCompose(in, "web")
	if err != nil {
		t.Fatal(err)
	}
	second, digest2, err := GenCompose(in, "web")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || digest1 != digest2 {
		t.Error("compose generation must be deterministic")
	}
}

func TestGenComposeInvariants(t *testing.T) {
	in := fixtureInput(t)
	content, _, err := GenCompose(in, "web")
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)

	// Secret values must never appear; only the env-file path may.
	if !strings.Contains(s, "/var/lib/bastion/secrets/testbox/web.env") {
		t.Error("secret env_file path missing")
	}
	if strings.Contains(s, "API_TOKEN") {
		t.Error("secret-backed variables must not appear in the compose file")
	}
	// Private endpoints bind the VM loopback only.
	if !strings.Contains(s, `"127.0.0.1:3000:3000"`) {
		t.Error("private endpoint must publish on 127.0.0.1 only")
	}
	if strings.Contains(s, "0.0.0.0") {
		t.Error("nothing may bind 0.0.0.0")
	}
	// Isolation defaults.
	if !strings.Contains(s, "no-new-privileges:true") {
		t.Error("no-new-privileges must be set")
	}
	// Ownership labels.
	for _, label := range []string{"bastion.box-id:", "bastion.service:", "bastion.config-digest:"} {
		if !strings.Contains(s, label) {
			t.Errorf("missing label %s", label)
		}
	}
	// No cross-project depends_on — ordering is the engine's job (Δ13).
	if strings.Contains(s, "depends_on") {
		t.Error("depends_on must not appear in generated compose projects")
	}
}

func TestServiceApplyOrder(t *testing.T) {
	in := fixtureInput(t)
	order := serviceApplyOrder(in.Box)
	if strings.Join(order, ",") != "db,web" {
		t.Errorf("order = %v (db must precede its dependent web)", order)
	}
}

func TestMutableTag(t *testing.T) {
	cases := map[string]bool{
		"ghcr.io/example/web:1.4.2": false,
		"ghcr.io/example/web@sha256:0011223344556677889900112233445566778899001122334455667788990011": false,
		"ghcr.io/example/web:latest":    true,
		"ghcr.io/example/web":           true,
		"postgres":                      true,
		"registry.example.com:5000/app": true, // port, no tag
	}
	for image, want := range cases {
		if got := mutableTag(image); got != want {
			t.Errorf("mutableTag(%q) = %v, want %v", image, got, want)
		}
	}
}

func TestRestartPolicyNoIsQuoted(t *testing.T) {
	in := fixtureInput(t)
	svc := in.Box.Services["db"]
	svc.RestartPolicy = "no"
	in.Box.Services["db"] = svc
	content, _, err := GenCompose(in, "db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `restart: "no"`) {
		t.Error(`restart "no" must be quoted so YAML does not read it as false`)
	}
}
