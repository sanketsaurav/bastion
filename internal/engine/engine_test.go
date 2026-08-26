package engine

import (
	"context"
	"encoding/base64"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixtureYAML = `apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: testbox
provider:
  name: gcp
  project: proj
  zone: us-west1-a
  instance: vm
host:
  packages: [git, jq]
  features:
    - uses: docker
    - uses: tmux
    - uses: ./features/mytool
      with:
        channel: stable
  files:
    - source: files/tmux.conf
      target: ~/.tmux.conf
      mode: replace
      permissions: "0600"
  shell:
    prompt: dev
volumes:
  data:
    persistence: durable
  scratch:
    persistence: ephemeral
secrets:
  token:
    source:
      environment: TEST_TOKEN
services:
  db:
    image: docker.io/library/postgres:16.4
    healthcheck:
      command: ["pg_isready"]
      interval: 10s
    mounts:
      - volume: data
        target: /var/lib/postgresql/data
  web:
    image: ghcr.io/example/web@sha256:0011223344556677889900112233445566778899001122334455667788990011
    dependsOn: [db]
    environment:
      LOG_LEVEL: info
      API_TOKEN:
        secretRef: token
    mounts:
      - volume: scratch
        target: /tmp/cache
    endpoints:
      http:
        containerPort: 3000
        visibility: private
`

const fixtureTmuxConf = "set -g mouse on\n"

func fixtureInput(t *testing.T) *Input {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("bastion.yaml", fixtureYAML, 0o644)
	write("files/tmux.conf", fixtureTmuxConf, 0o644)
	write("features/mytool/feature.yaml", "name: mytool\nversion: \"2\"\n", 0o644)
	write("features/mytool/check", "#!/bin/sh\nexit 0\n", 0o755)
	write("features/mytool/apply", "#!/bin/sh\nexit 0\n", 0o755)

	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	return &Input{Box: loaded.Box, Dir: loaded.Dir, BoxID: BoxID(loaded.Box), Version: "test"}
}

func b64s(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run `go test ./internal/engine -update`): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from golden; run with -update and review the diff", name)
	}
}

// fakeSession replays canned protocol lines and captures the script.
type fakeSession struct {
	lines  []string
	exit   int
	script []byte
}

func (f *fakeSession) RunScript(_ context.Context, script []byte, onLine func(string)) (execx.Result, error) {
	f.script = script
	for _, l := range f.lines {
		onLine(l)
	}
	return execx.Result{ExitCode: f.exit}, nil
}
