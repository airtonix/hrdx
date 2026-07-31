package holder

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRing(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abc"))
	if got := string(r.Bytes()); got != "abc" {
		t.Fatalf("ring = %q, want abc", got)
	}
	r.Write([]byte("defgh"))
	if got := string(r.Bytes()); got != "abcdefgh" {
		t.Fatalf("ring = %q, want abcdefgh", got)
	}
	// Overflow evicts the oldest bytes.
	r.Write([]byte("XY"))
	if got := string(r.Bytes()); got != "cdefghXY" {
		t.Fatalf("ring = %q, want cdefghXY", got)
	}
	// A write larger than the buffer keeps only the tail.
	r.Write([]byte("0123456789"))
	if got := string(r.Bytes()); got != "23456789" {
		t.Fatalf("ring = %q, want 23456789", got)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	in := frame{Type: TOut, ID: 42, Payload: []byte("hello")}
	if err := writeFrame(&buffer, in); err != nil {
		t.Fatal(err)
	}
	out, err := readFrame(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.ID != in.ID || string(out.Payload) != "hello" {
		t.Fatalf("frame = %+v", out)
	}
	// Empty payload.
	if err := writeFrame(&buffer, frame{Type: TCtrl, ID: 0}); err != nil {
		t.Fatal(err)
	}
	if out, err = readFrame(&buffer); err != nil || out.Type != TCtrl || len(out.Payload) != 0 {
		t.Fatalf("empty frame = %+v err=%v", out, err)
	}
}

// shortSocketPath returns a socket path under the system temp dir; the
// deep t.TempDir() paths exceed the 104-byte sun_path limit on macOS.
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hrdx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "h.sock")
}

// startTestHolder runs a holder server on a temp socket.
func startTestHolder(t *testing.T) string {
	t.Helper()
	socket := shortSocketPath(t)
	server := NewServer(socket, "test")
	go func() { _ = server.Run() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder socket never appeared")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() {
		if client, err := Connect(socket); err == nil {
			client.Shutdown()
			client.Close()
		}
	})
	return socket
}

// collector gathers session output thread-safely.
type collector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *collector) sink(data []byte) {
	c.mu.Lock()
	c.buf.Write(data)
	c.mu.Unlock()
}

func (c *collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func waitContains(t *testing.T, c *collector, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(c.String(), needle) {
		if time.Now().After(deadline) {
			t.Fatalf("output %q never contained %q", c.String(), needle)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHolderSessionSurvivesDetach(t *testing.T) {
	socket := startTestHolder(t)

	// First client: start a shell, see its output.
	first, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Start("/bin/sh", []string{"-c", "echo ready; cat"}, t.TempDir(), []string{"PATH=/bin:/usr/bin"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// Let the marker arrive before Attach. Output from a started but not yet
	// subscribed session must be buffered rather than dropped.
	time.Sleep(100 * time.Millisecond)
	var out1 collector
	if _, err := first.Attach(session, 80, 24, out1.sink); err != nil {
		t.Fatal(err)
	}
	waitContains(t, &out1, "ready")

	// Detach. The session keeps running; output lands in the ring.
	first.Close()
	time.Sleep(100 * time.Millisecond)

	// Second client: the session is still there, input still works.
	second, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	sessions, err := second.List()
	if err != nil || len(sessions) != 1 || !sessions[0].Running {
		t.Fatalf("sessions after detach = %+v err=%v", sessions, err)
	}
	var out2 collector
	running, err := second.Attach(session, 80, 24, out2.sink)
	if err != nil || !running {
		t.Fatalf("reattach = %v err=%v", running, err)
	}
	second.Write(session, []byte("echoed-back\n"))
	waitContains(t, &out2, "echoed-back")
}

func TestHolderReplaysDetachedOutput(t *testing.T) {
	socket := startTestHolder(t)

	first, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	// The marker prints shortly after attach; we detach before it fires.
	session, err := first.Start("/bin/sh", []string{"-c", "sleep 0.3; echo late-marker; sleep 30"}, t.TempDir(), []string{"PATH=/bin:/usr/bin"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	time.Sleep(600 * time.Millisecond) // marker printed while detached

	second, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var out collector
	if _, err := second.Attach(session, 80, 24, out.sink); err != nil {
		t.Fatal(err)
	}
	waitContains(t, &out, "late-marker")
}

func TestHolderExitEvent(t *testing.T) {
	socket := startTestHolder(t)
	client, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	exited := make(chan int64, 1)
	client.SetExitHandler(func(session int64) { exited <- session })

	session, err := client.Start("/bin/sh", []string{"-c", "exit 0"}, t.TempDir(), []string{"PATH=/bin:/usr/bin"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	var out collector
	_, _ = client.Attach(session, 80, 24, out.sink)

	select {
	case got := <-exited:
		if got != session {
			t.Fatalf("exit event for %d, want %d", got, session)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exit event")
	}
}

func TestHolderProtocolMismatch(t *testing.T) {
	socket := startTestHolder(t)
	conn, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A raw wrong-protocol hello must be rejected.
	if _, err := conn.call(request{Op: "hello", Protocol: Protocol + 1}); err == nil {
		t.Fatal("wrong protocol accepted")
	}
}

func TestHolderKill(t *testing.T) {
	socket := startTestHolder(t)
	client, err := Connect(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	session, err := client.Start("/bin/sh", []string{"-c", "sleep 30"}, t.TempDir(), []string{"PATH=/bin:/usr/bin"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	client.Kill(session)
	deadline := time.Now().Add(5 * time.Second)
	for {
		sessions, _ := client.List()
		if len(sessions) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session not removed: %+v", sessions)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
