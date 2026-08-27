package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const otherConfig = `Host *
  IdentityAgent agent.sock

Host unrelated
  HostName example.com
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSpliceAppendsAndReplaces(t *testing.T) {
	path := writeConfig(t, otherConfig)
	stanza := "# bastion:dev — managed\nHost dev\n  User x\n# end bastion:dev"

	if _, err := spliceSSHConfig(path, "dev", stanza); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Host unrelated") || !strings.Contains(string(data), "User x") {
		t.Fatalf("append lost content:\n%s", data)
	}

	// Replacing rewrites the block in place, never duplicates it.
	updated := strings.Replace(stanza, "User x", "User y", 1)
	if _, err := spliceSSHConfig(path, "dev", updated); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "User x") || strings.Count(string(data), "Host dev") != 1 {
		t.Fatalf("replace failed:\n%s", data)
	}

	// A hand-written begin marker with trailing prose is still ours.
	hand := strings.Replace(string(data), "# bastion:dev — managed", "# bastion:dev — reaches the box over IAP", 1)
	os.WriteFile(path, []byte(hand), 0o600)
	if _, err := spliceSSHConfig(path, "dev", stanza); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "User x") || strings.Count(string(data), "Host dev") != 1 {
		t.Fatalf("marker-prefix replace failed:\n%s", data)
	}

	// Removal deletes exactly the block.
	changed, err := spliceSSHConfig(path, "dev", "")
	if err != nil || !changed {
		t.Fatalf("remove: changed=%v err=%v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "bastion:dev") || !strings.Contains(string(data), "Host unrelated") {
		t.Fatalf("remove failed:\n%s", data)
	}

	// A similarly named box never matches this box's markers.
	if _, err := spliceSSHConfig(path, "dev", stanza); err != nil {
		t.Fatal(err)
	}
	if changed, _ := spliceSSHConfig(path, "dev2", ""); changed {
		t.Error("dev2 must not match dev's markers")
	}
}

func TestSpliceRefusesLostEndMarker(t *testing.T) {
	path := writeConfig(t, "# bastion:dev — managed\nHost dev\n  User x\n\nHost precious\n  HostName keep.me\n")
	if _, err := spliceSSHConfig(path, "dev", "new"); err == nil ||
		!strings.Contains(err.Error(), "end marker") {
		t.Fatalf("expected an end-marker error, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "precious") {
		t.Fatal("file must be untouched on refusal")
	}
}

func TestSpliceCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config")
	if _, err := spliceSSHConfig(path, "dev", "# bastion:dev\nHost dev\n# end bastion:dev"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created file mode = %v, err %v", info.Mode(), err)
	}
}
