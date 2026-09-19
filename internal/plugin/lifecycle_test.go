package plugin

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestRuntimeRejectsInvalidRegistration(t *testing.T) {
	runtime := NewRuntime([]Registration{{Package: Package{ID: "example.invalid"}}})
	defer runtime.Close()
	if statuses := runtime.Statuses(); len(statuses) != 1 || statuses[0].State != "blocked" {
		t.Fatalf("invalid registration status: %+v", statuses)
	}
	if err := runtime.Start("example.invalid"); err == nil {
		t.Fatal("invalid registration started")
	}
}

func TestRuntimeRevokesChangedApproval(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	base := t.TempDir()
	path := ApprovalPath(base, entry.Package.ID)
	if err := state.SavePluginApproval(path, entry.Approval); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime([]Registration{entry})
	runtime.WatchApprovals(base)
	t.Cleanup(runtime.Close)
	if _, err := runtime.Call(t.Context(), entry.Package.ID, "echo", nil); err != nil {
		t.Fatal(err)
	}
	entry.Approval.Grants = nil
	if err := state.SavePluginApproval(path, entry.Approval); err != nil {
		t.Fatal(err)
	}
	status := waitRuntimeState(t, runtime, "blocked")
	if _, ok := runtime.Authorize(Request{Plugin: entry.Package.ID, Generation: status.Generation, Context: t.Context()}, "ui.notification"); ok {
		t.Fatal("revoked grant remained active")
	}
	if err := runtime.Start(entry.Package.ID); err == nil {
		t.Fatal("restart resurrected revoked approval")
	}
}

func TestRuntimeShutdownUnblocksFullInputPipe(t *testing.T) {
	entry := runtimeFixture(t, "never-read")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	if err := runtime.Start(entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	waitRuntimeState(t, runtime, "ready")
	callDone := make(chan error, 1)
	go func() {
		_, err := runtime.Call(t.Context(), entry.Package.ID, "echo", strings.Repeat("x", 200*1024))
		callDone <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		runtime.mu.Lock()
		s := runtime.sessions[entry.Package.ID]
		runtime.mu.Unlock()
		s.mu.Lock()
		pending := len(s.calls)
		s.mu.Unlock()
		if pending > 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("call did not begin")
		}
	}
	closed := make(chan struct{})
	go func() { runtime.Close(); close(closed) }()
	select {
	case <-closed:
	case <-deadline.C:
		t.Fatal("full stdin pipe blocked shutdown")
	}
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("disconnected call succeeded")
		}
	case <-deadline.C:
		t.Fatal("pending caller leaked")
	}
}

// A peer that spawns a child must not leave it behind after shutdown. The
// helper spawns a copy of itself in "orphan" mode and reports its PID.
func TestRuntimeShutdownCleansManagedDescendants(t *testing.T) {
	entry := runtimeFixture(t, "spawn")
	runtime := NewRuntime([]Registration{entry})
	result, err := runtime.Call(t.Context(), entry.Package.ID, "child", nil)
	if err != nil {
		t.Fatal(err)
	}
	var child struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(result, &child) != nil || child.PID <= 0 {
		t.Fatalf("child pid: %s", result)
	}
	process, err := os.FindProcess(child.PID)
	if err != nil {
		t.Fatal(err)
	}
	if !processAlive(process) {
		t.Fatal("descendant did not start")
	}
	runtime.Close()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(process) {
		if time.Now().After(deadline) {
			t.Fatal("descendant outlived the plugin runtime")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRuntimeRapidStartStopReloadLeavesNoSession(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	base := t.TempDir()
	if err := state.SavePluginApproval(ApprovalPath(base, entry.Package.ID), entry.Approval); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime([]Registration{entry})
	runtime.WatchApprovals(base)
	t.Cleanup(runtime.Close)
	for round := 0; round < 5; round++ {
		if err := runtime.Start(entry.Package.ID); err != nil {
			t.Fatal(err)
		}
		runtime.Stop(entry.Package.ID)
		if err := runtime.Reload(t.Context(), entry.Package.ID); err != nil {
			t.Fatal(err)
		}
	}
	waitRuntimeState(t, runtime, "ready")
	// Shutdown during activation: start, then close before ready.
	runtime.Stop(entry.Package.ID)
	if err := runtime.Restart(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { runtime.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("close during activation hung")
	}
	if status := runtime.Statuses()[0]; status.State != "stopped" {
		t.Fatalf("final state %+v", status)
	}
}

func TestRuntimeFloodDisconnectsNoisyPeer(t *testing.T) {
	entry := runtimeFixture(t, "flood")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	if err := runtime.Start(entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	// Which limit trips first depends on scheduling: the inbound frame rate,
	// the outstanding host-request cap, or the output queue that fills with
	// responses. All of them end the session without blocking the host.
	status := waitRuntimeState(t, runtime, "failed")
	if status.Error == "" {
		t.Fatalf("flood was not bounded: %+v", status)
	}
	if _, ok := runtime.Authorize(Request{Plugin: entry.Package.ID, Generation: status.Generation, Context: t.Context()}, "ui.notification"); ok {
		t.Fatal("failed flooding session still authorized")
	}
	// The bounded event bridge must not have blocked: draining it now must
	// finish immediately.
	for {
		select {
		case <-runtime.Events():
			continue
		default:
		}
		break
	}
}

// BenchmarkRuntimeRoundTrip records the baseline cost of one host-to-peer
// call over the stdio transport. Run with:
//
//	go test ./internal/plugin -run xxx -bench RoundTrip -benchmem
func BenchmarkRuntimeRoundTrip(b *testing.B) {
	entry := runtimeFixture(b, "normal")
	runtime := NewRuntime([]Registration{entry})
	b.Cleanup(runtime.Close)
	if _, err := runtime.Activate(b.Context(), entry.Package.ID); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runtime.Call(b.Context(), entry.Package.ID, "echo", i); err != nil {
			b.Fatal(err)
		}
		// Stay under the 256 inbound frames per second limit, which is part of
		// the contract being measured rather than a benchmark artifact.
		if (i+1)%200 == 0 {
			b.StopTimer()
			time.Sleep(time.Second)
			b.StartTimer()
		}
	}
}

// A disabled runtime (the default) must add no goroutines or channels to the
// host: NewRuntime is never called, so the test asserts the zero-value path
// in the UI package instead. Here we bound the enabled runtime's idle cost.
func TestRuntimeIdleHasBoundedGoroutines(t *testing.T) {
	before := goroutineCount()
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	if _, err := runtime.Activate(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	during := goroutineCount()
	if during-before > 8 {
		t.Fatalf("ready plugin added %d goroutines, want at most 8", during-before)
	}
	runtime.Close()
	deadline := time.Now().Add(5 * time.Second)
	for goroutineCount() > before+1 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines leaked after close: before=%d after=%d", before, goroutineCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func goroutineCount() int { return runtime.NumGoroutine() }
