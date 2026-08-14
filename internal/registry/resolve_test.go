package registry

import (
	"strings"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	flagDir := writeBoxDir(t, "flagbox")
	envDir := writeBoxDir(t, "envbox")
	regDir := writeBoxDir(t, "regbox")
	cwd := writeBoxDir(t, "cwdbox")
	emptyCWD := t.TempDir()

	reg := testRegistry(t)
	reg.Client.Boxes["regbox"] = regDir

	t.Run("--config wins over everything", func(t *testing.T) {
		res, err := reg.Resolve(Request{ConfigFlag: flagDir, EnvConfig: envDir, CWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		if res.Loaded.Box.Metadata.Name != "flagbox" || res.Source != SourceFlag {
			t.Errorf("got %q from %q", res.Loaded.Box.Metadata.Name, res.Source)
		}
	})

	t.Run("--config with mismatched name fails", func(t *testing.T) {
		if _, err := reg.Resolve(Request{ConfigFlag: flagDir, Name: "other", CWD: emptyCWD}); err == nil {
			t.Fatal("expected a name-mismatch error")
		}
	})

	t.Run("BASTION_CONFIG beats cwd", func(t *testing.T) {
		res, err := reg.Resolve(Request{EnvConfig: envDir, CWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		if res.Loaded.Box.Metadata.Name != "envbox" || res.Source != SourceEnv {
			t.Errorf("got %q from %q", res.Loaded.Box.Metadata.Name, res.Source)
		}
	})

	t.Run("name resolves through registry", func(t *testing.T) {
		res, err := reg.Resolve(Request{Name: "regbox", CWD: emptyCWD})
		if err != nil {
			t.Fatal(err)
		}
		if res.Source != SourceRegistry {
			t.Errorf("source = %q", res.Source)
		}
	})

	t.Run("name matching cwd box uses cwd", func(t *testing.T) {
		res, err := reg.Resolve(Request{Name: "cwdbox", CWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		if res.Source != SourceCWD {
			t.Errorf("source = %q", res.Source)
		}
	})

	t.Run("cwd bastion.yaml without name", func(t *testing.T) {
		res, err := reg.Resolve(Request{CWD: cwd})
		if err != nil {
			t.Fatal(err)
		}
		if res.Loaded.Box.Metadata.Name != "cwdbox" || res.Source != SourceCWD {
			t.Errorf("got %q from %q", res.Loaded.Box.Metadata.Name, res.Source)
		}
	})

	t.Run("current box fallback", func(t *testing.T) {
		reg.Client.CurrentBox = "regbox"
		defer func() { reg.Client.CurrentBox = "" }()
		res, err := reg.Resolve(Request{CWD: emptyCWD})
		if err != nil {
			t.Fatal(err)
		}
		if res.Loaded.Box.Metadata.Name != "regbox" || res.Source != SourceCurrent {
			t.Errorf("got %q from %q", res.Loaded.Box.Metadata.Name, res.Source)
		}
	})

	t.Run("nothing selected", func(t *testing.T) {
		_, err := reg.Resolve(Request{CWD: emptyCWD})
		if err == nil || !strings.Contains(err.Error(), "no box selected") {
			t.Fatalf("expected a no-box-selected error, got: %v", err)
		}
	})

	t.Run("unknown name lists registered boxes", func(t *testing.T) {
		_, err := reg.Resolve(Request{Name: "ghost", CWD: emptyCWD})
		if err == nil || !strings.Contains(err.Error(), "regbox") {
			t.Fatalf("expected the error to list registered boxes, got: %v", err)
		}
	})
}

func TestResolveAmbiguityIsAnError(t *testing.T) {
	cwd := writeBoxDir(t, "agents")
	other := writeBoxDir(t, "agents")

	reg := testRegistry(t)
	reg.Client.Boxes["agents"] = other

	_, err := reg.Resolve(Request{Name: "agents", CWD: cwd})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected an ambiguity error, got: %v", err)
	}

	// Same path on both sides is not ambiguous.
	reg.Client.Boxes["agents"] = cwd
	res, err := reg.Resolve(Request{Name: "agents", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != SourceCWD {
		t.Errorf("source = %q", res.Source)
	}
}

func TestResolveRegistryNameDrift(t *testing.T) {
	dir := writeBoxDir(t, "renamed")
	reg := testRegistry(t)
	reg.Client.Boxes["agents"] = dir

	_, err := reg.Resolve(Request{Name: "agents", CWD: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "re-adopt") {
		t.Fatalf("expected a re-adopt error, got: %v", err)
	}
}
