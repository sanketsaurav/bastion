package execx

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Cancellation must reach grandchildren: the transport wraps ssh (gcloud →
// ssh), and interrupting only the direct child leaves the connection — and
// with it the remote runner — alive and unmonitored.
func TestRunStreamCancelKillsGrandchildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pidLine string
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Local{}.RunStream(ctx, []string{"sh", "-c", "sleep 30 & echo $!; wait"}, nil, func(l string) {
			if pidLine == "" {
				pidLine = strings.TrimSpace(l)
				cancel()
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("RunStream did not return after cancellation")
	}
	pid, err := strconv.Atoi(pidLine)
	if err != nil || pid <= 0 {
		t.Fatalf("no grandchild pid observed (%q)", pidLine)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("grandchild survived cancellation")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
