package execx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Cancellation must stop the whole process tree: the transport wraps ssh
// (gcloud → ssh), and interrupting only the direct child leaves the
// connection — and with it the remote runner — alive and unmonitored. The
// grandchild runs in the foreground (a shell background job would start
// with SIGINT ignored, per POSIX) and liveness is observed through a tick
// file, so unreaped zombies under container init cannot fake survival.
func TestRunStreamCancelStopsGrandchildren(t *testing.T) {
	tick := filepath.Join(t.TempDir(), "ticks")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Local{}.RunStream(ctx, []string{
			"sh", "-c", `sh -c 'while :; do echo t >>"$1"; sleep 0.1; done' inner "$1"`, "outer", tick,
		}, nil, func(string) {})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if fi, err := os.Stat(tick); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("grandchild never started writing")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("RunStream did not return after cancellation")
	}
	time.Sleep(300 * time.Millisecond)
	before, err := os.Stat(tick)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	after, err := os.Stat(tick)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("grandchild kept writing after cancellation (%d → %d bytes)", before.Size(), after.Size())
	}
}
