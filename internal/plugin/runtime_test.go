package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/patriceckhart/hrdx/internal/state"
)

func runtimeFixture(t testing.TB, mode string) Registration {
	t.Helper()
	root := t.TempDir()
	path := packageFixture(t, root, "peer", "example.peer")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(path, "bin", "plugin.exe"), binary, 0o700)
	manifest := strings.ReplaceAll(validManifest, "example.git-tools", "example.peer")
	manifest = strings.ReplaceAll(manifest, "bin/plugin", "bin/plugin.exe")
	manifest = strings.Replace(manifest, `"--stdio"`, `"-test.run=^TestPluginPeer$", "--", "`+mode+`"`, 1)
	writeFixture(t, filepath.Join(path, ManifestFile), []byte(manifest), 0o600)
	entry := InspectPackage(path)
	approval, err := Approve(entry, []string{"ui.action.contribute", "ui.notification"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return Registration{Package: entry, Approval: approval}
}

func TestPluginPeer(t *testing.T) {
	id := os.Getenv("HRDX_PLUGIN_ID")
	if id != "example.peer" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	encoder := json.NewEncoder(os.Stdout)
	emit := func(frame Frame) {
		if encoder.Encode(frame) != nil {
			os.Exit(2)
		}
	}
	if mode == "orphan" {
		// A descendant that ignores stdin and never exits on its own.
		<-time.After(time.Hour)
		os.Exit(0)
	}
	if mode == "badhello" {
		id = "example.spoofed"
	}
	emit(Frame{Type: "hello", Plugin: id, Protocol: ProtocolVersion})
	var waiting string
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes+1)
	for scanner.Scan() {
		var frame Frame
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		switch frame.Type {
		case "hello_ack":
			if mode != "unready" {
				emit(Frame{Type: "ready"})
			}
			if mode == "never-read" {
				<-time.After(time.Hour)
			}
			if mode == "flood" {
				for i := 0; i < 1000; i++ {
					emit(Frame{Type: "request", ID: fmt.Sprint(i), Method: "ui.notify", Params: json.RawMessage(`{"text":"flood"}`)})
				}
				<-time.After(time.Hour)
			}
			if mode == "orphan" {
				<-time.After(time.Hour)
			}
		case "request":
			switch frame.Method {
			case "crash":
				os.Exit(7)
			case "child":
				self, _ := os.Executable()
				child := exec.Command(self, "-test.run=^TestPluginPeer$", "--", "orphan")
				child.Env = append(os.Environ(), "HRDX_PLUGIN_ID=example.peer")
				if child.Start() != nil {
					os.Exit(8)
				}
				emit(Frame{Type: "response", ID: frame.ID, Result: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, child.Process.Pid))})
			case "hang":
			case "host":
				waiting = frame.ID
				emit(Frame{Type: "request", ID: "host-1", Method: "ui.notify", Params: json.RawMessage(`{"text":"synthetic notification"}`)})
			default:
				emit(Frame{Type: "response", ID: frame.ID, Result: frame.Params})
			}
		case "response":
			emit(Frame{Type: "response", ID: waiting, Result: frame.Result, Error: frame.Error})
		case "shutdown":
			if mode == "ignore-shutdown" {
				continue
			}
			emit(Frame{Type: "shutdown_ack"})
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func waitRuntimeState(t *testing.T, runtime *Runtime, wanted string) Status {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		for _, status := range runtime.Statuses() {
			if status.State == wanted {
				return status
			}
		}
		select {
		case <-runtime.Events():
		case <-deadline.C:
			t.Fatalf("wanted %s, got %+v", wanted, runtime.Statuses())
		}
	}
}

func TestRuntimeHandshakeCallsAndShutdown(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	if got := runtime.Statuses()[0].State; got != "approved" {
		t.Fatal(got)
	}
	result, err := runtime.Call(t.Context(), entry.Package.ID, "echo", map[string]string{"text": "hello"})
	if err != nil || string(result) != `{"text":"hello"}` {
		t.Fatalf("call: %s %v", result, err)
	}
	generation := waitRuntimeState(t, runtime, "ready").Generation
	request := Request{Plugin: entry.Package.ID, Generation: generation, Context: t.Context()}
	if _, ok := runtime.Authorize(request, "ui.notification"); !ok {
		t.Fatal("granted notification denied")
	}
	if _, ok := runtime.Authorize(request, "pane.read_screen"); ok {
		t.Fatal("ungranted screen access")
	}
	request.Generation = "stale"
	if _, ok := runtime.Authorize(request, "ui.notification"); ok {
		t.Fatal("stale generation authorized")
	}
	runtime.Stop(entry.Package.ID)
	if _, err := runtime.Call(t.Context(), entry.Package.ID, "echo", nil); err == nil {
		t.Fatal("pending invocation restarted stopped plugin")
	}
	request.Generation = generation
	if _, ok := runtime.Authorize(request, "ui.notification"); ok {
		t.Fatal("stopped session authorized")
	}
	runtime.Close()
	if runtime.Statuses()[0].State != "stopped" {
		t.Fatalf("shutdown: %+v", runtime.Statuses())
	}
}

func TestRuntimeCallGenerationRejectsReplacement(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	generation, err := runtime.Activate(t.Context(), entry.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Restart(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	waitRuntimeState(t, runtime, "ready")
	if _, err := runtime.CallGeneration(t.Context(), entry.Package.ID, generation, "echo", nil); err == nil {
		t.Fatal("stale generation redirected call to replacement process")
	}
}

func TestRuntimeConcurrentCalls(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func(value int) {
			defer workers.Done()
			result, err := runtime.Call(t.Context(), entry.Package.ID, "echo", value)
			var got int
			if err != nil || json.Unmarshal(result, &got) != nil || got != value {
				t.Errorf("call %d: %s %v", value, result, err)
			}
		}(index)
	}
	workers.Wait()
}

func TestRuntimeHostRequestAndCancellation(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	finished := make(chan error, 1)
	go func() { _, err := runtime.Call(t.Context(), entry.Package.ID, "host", nil); finished <- err }()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	var request *Request
	for request == nil {
		select {
		case event := <-runtime.Events():
			request = event.Request
		case <-deadline.C:
			t.Fatal("missing host request")
		}
	}
	if _, ok := runtime.Authorize(*request, "ui.notification"); !ok {
		t.Fatal("request not authorized")
	}
	request.Reply <- Reply{Result: map[string]bool{"ok": true}}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runtime.Call(ctx, entry.Package.ID, "hang", nil); err == nil {
		t.Fatal("canceled call succeeded")
	}
	runtime.mu.Lock()
	s := runtime.sessions[entry.Package.ID]
	runtime.mu.Unlock()
	s.mu.Lock()
	pending := len(s.calls)
	s.mu.Unlock()
	if pending != 0 {
		t.Fatal("pending calls leaked")
	}
}

func TestRuntimeCrashRestartAndChangedPackage(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	runtime := NewRuntime([]Registration{entry})
	t.Cleanup(runtime.Close)
	if _, err := runtime.Call(t.Context(), entry.Package.ID, "crash", nil); err == nil {
		t.Fatal("crash should fail call")
	}
	old := waitRuntimeState(t, runtime, "failed").Generation
	if err := runtime.Restart(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitRuntimeState(t, runtime, "ready").Generation; got == old {
		t.Fatal("restart reused generation")
	}
	runtime.Stop(entry.Package.ID)
	writeFixture(t, filepath.Join(entry.Package.Path, "new-asset"), []byte("changed"), 0o600)
	if err := runtime.Restart(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	waitRuntimeState(t, runtime, "blocked")
}

func TestRuntimeReloadsOnlyReapprovedPackage(t *testing.T) {
	entry := runtimeFixture(t, "normal")
	base := t.TempDir()
	if err := state.SavePluginApproval(ApprovalPath(base, entry.Package.ID), entry.Approval); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime([]Registration{entry})
	runtime.WatchApprovals(base)
	t.Cleanup(runtime.Close)
	old, err := runtime.Activate(t.Context(), entry.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(entry.Package.Path, "new-asset"), []byte("changed"), 0o600)
	if err := runtime.Reload(t.Context(), entry.Package.ID); err == nil {
		t.Fatal("changed package reloaded without reapproval")
	}
	refreshed := InspectPackage(entry.Package.Path)
	approval, err := Approve(refreshed, entry.Approval.Grants, entry.Approval.Workspaces, entry.Approval.Instance)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SavePluginApproval(ApprovalPath(base, entry.Package.ID), approval); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Reload(t.Context(), entry.Package.ID); err != nil {
		t.Fatal(err)
	}
	status := waitRuntimeState(t, runtime, "ready")
	if status.Generation == old {
		t.Fatal("reload retained old generation")
	}
}

func TestRuntimeRejectsBadHelloAndBoundsShutdown(t *testing.T) {
	for _, mode := range []string{"badhello", "unready", "ignore-shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entry := runtimeFixture(t, mode)
			runtime := NewRuntime([]Registration{entry})
			t.Cleanup(runtime.Close)
			if err := runtime.Start(entry.Package.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "badhello" {
				waitRuntimeState(t, runtime, "failed")
			} else if mode == "ignore-shutdown" {
				waitRuntimeState(t, runtime, "ready")
			} else {
				waitRuntimeState(t, runtime, "handshaking")
			}
			done := make(chan struct{})
			go func() { runtime.Close(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown blocked")
			}
		})
	}
}

func TestProtocolArbitraryReadBoundaries(t *testing.T) {
	count := 0
	input := "{\"type\":\"hello\",\"plugin\":\"example.test\",\"protocol\":1}\r\n{\"type\":\"ready\"}\n"
	err := readFrames(iotest.OneByteReader(strings.NewReader(input)), func(Frame) bool { count++; return true })
	if err != io.EOF || count != 2 {
		t.Fatalf("framing: count=%d err=%v", count, err)
	}
}

func FuzzReadFrames(f *testing.F) {
	f.Add("{\"type\":\"ready\"}\n")
	f.Add("{\"type\":\"request\",\"id\":\"1\",\"method\":\"status\"}\n")
	f.Fuzz(func(t *testing.T, input string) {
		_ = readFrames(strings.NewReader(input), func(frame Frame) bool {
			if frame.Type == "" || len(frame.ID) > 128 {
				t.Fatal("invalid frame delivered")
			}
			return true
		})
	})
}

func TestProtocolRejectsAmbiguousFrames(t *testing.T) {
	for _, input := range []string{"{}\n", "[]\n", `{"type":"hello"}`, "{\"type\":\"response\"}\n", "{\"type\":\"hello\",\"type\":\"ready\"}\n", strings.Repeat("x", MaxFrameBytes+1) + "\n"} {
		if err := readFrames(strings.NewReader(input), func(Frame) bool { t.Fatal("invalid frame delivered"); return true }); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
}
