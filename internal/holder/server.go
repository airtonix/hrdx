package holder

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// ringCapacity bounds detached output per session. 1 MiB comfortably
// holds a full screen plus the scrollback a vt10x replay can use.
const ringCapacity = 1 << 20

// session is one PTY-owning subprocess inside the holder.
type session struct {
	id      int64
	command string
	cwd     string
	ptmx    *os.File
	cmd     *exec.Cmd
	running bool

	mu     sync.Mutex
	buffer *ring // detached output, replayed on attach

	// fgName caches the foreground process lookup.
	fgName      string
	fgCheckedAt time.Time
}

// Server is the holder process: it owns sessions and serves exactly one
// TUI client at a time.
type Server struct {
	socket  string
	version string

	mu       sync.Mutex
	sessions map[int64]*session
	nextID   int64
	client   net.Conn // current attached client, nil when detached
	clientMu sync.Mutex

	listener net.Listener
	done     chan struct{}
}

// NewServer prepares a holder for the given socket path.
func NewServer(socket, version string) *Server {
	return &Server{
		socket:   socket,
		version:  version,
		sessions: map[int64]*session{},
		nextID:   1,
		done:     make(chan struct{}),
	}
}

// Run listens and serves until a shutdown request arrives. The socket
// file is removed on exit.
func (s *Server) Run() error {
	_ = os.MkdirAll(filepath.Dir(s.socket), 0o755)
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("holder listen: %w", err)
	}
	s.listener = listener
	defer func() {
		listener.Close()
		_ = os.Remove(s.socket)
	}()

	go func() {
		<-s.done
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return err
			}
		}
		s.serveClient(conn)
	}
}

// serveClient handles one TUI connection until it disconnects. A new
// connection replaces the previous one (the old TUI is gone or dying).
func (s *Server) serveClient(conn net.Conn) {
	s.clientMu.Lock()
	if s.client != nil {
		_ = s.client.Close()
	}
	s.client = conn
	s.clientMu.Unlock()

	defer func() {
		s.clientMu.Lock()
		if s.client == conn {
			s.client = nil
		}
		s.clientMu.Unlock()
		_ = conn.Close()
	}()

	for {
		f, err := readFrame(conn)
		if err != nil {
			return
		}
		switch f.Type {
		case TIn:
			s.mu.Lock()
			target := s.sessions[f.ID]
			s.mu.Unlock()
			if target != nil && target.running {
				_, _ = target.ptmx.Write(f.Payload)
			}
		case TCtrl:
			var req request
			if err := unmarshal(f.Payload, &req); err != nil {
				continue
			}
			if req.Op == "shutdown" {
				s.reply(conn, response{Req: req.Req})
				s.shutdown()
				return
			}
			s.reply(conn, s.handle(req))
		}
	}
}

func (s *Server) handle(req request) response {
	switch req.Op {
	case "hello":
		if req.Protocol != Protocol {
			return response{Req: req.Req, Err: fmt.Sprintf("protocol mismatch: holder %d, client %d", Protocol, req.Protocol)}
		}
		return response{Req: req.Req, Version: s.version, Protocol: Protocol}

	case "start":
		id, err := s.start(req)
		if err != nil {
			return response{Req: req.Req, Err: err.Error()}
		}
		return response{Req: req.Req, Session: id, Running: true}

	case "attach":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		if req.Cols > 1 && req.Rows > 1 && target.running {
			_ = pty.Setsize(target.ptmx, &pty.Winsize{Cols: uint16(req.Cols), Rows: uint16(req.Rows)})
		}
		// Replay buffered output so the client can rebuild the screen.
		target.mu.Lock()
		replay := target.buffer.Bytes()
		target.buffer = newRing(ringCapacity)
		target.mu.Unlock()
		if len(replay) > 0 {
			s.sendToClient(frame{Type: TOut, ID: target.id, Payload: replay})
		}
		return response{Req: req.Req, Session: target.id, Running: target.running}

	case "resize":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		if target.running && req.Cols > 1 && req.Rows > 1 {
			_ = pty.Setsize(target.ptmx, &pty.Winsize{Cols: uint16(req.Cols), Rows: uint16(req.Rows)})
		}
		return response{Req: req.Req}

	case "kill":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		s.kill(target)
		return response{Req: req.Req}

	case "fg":
		target := s.session(req.Session)
		if target == nil {
			return response{Req: req.Req, Err: fmt.Sprintf("session %d not found", req.Session)}
		}
		return response{Req: req.Req, Name: target.foreground()}

	case "list":
		s.mu.Lock()
		defer s.mu.Unlock()
		list := make([]SessionInfo, 0, len(s.sessions))
		for _, current := range s.sessions {
			list = append(list, SessionInfo{
				ID: current.id, Command: current.command, CWD: current.cwd, Running: current.running,
			})
		}
		return response{Req: req.Req, Sessions: list}
	}
	return response{Req: req.Req, Err: "unknown op " + req.Op}
}

// start launches a subprocess on a fresh PTY.
func (s *Server) start(req request) (int64, error) {
	cols, rows := req.Cols, req.Rows
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = req.CWD
	cmd.Env = req.Env
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", req.Command, err)
	}

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	target := &session{
		id:      id,
		command: req.Command,
		cwd:     req.CWD,
		ptmx:    ptmx,
		cmd:     cmd,
		running: true,
		buffer:  newRing(ringCapacity),
	}
	s.sessions[id] = target
	s.mu.Unlock()

	go s.pump(target)
	return id, nil
}

// pump reads one session's PTY forever, forwarding output to the
// attached client or buffering it while detached.
func (s *Server) pump(target *session) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := target.ptmx.Read(buffer)
		if n > 0 {
			payload := make([]byte, n)
			copy(payload, buffer[:n])
			if !s.sendToClient(frame{Type: TOut, ID: target.id, Payload: payload}) {
				target.mu.Lock()
				target.buffer.Write(payload)
				target.mu.Unlock()
			}
		}
		if err != nil {
			break
		}
	}
	_ = target.cmd.Wait()
	s.mu.Lock()
	target.running = false
	s.mu.Unlock()
	s.sendToClient(frame{Type: TEvt, Payload: marshal(event{Event: "exited", Session: target.id})})
}

// sendToClient writes a frame to the attached client. Returns false when
// no client is attached or the write failed.
func (s *Server) sendToClient(f frame) bool {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.client == nil {
		return false
	}
	if err := writeFrame(s.client, f); err != nil {
		_ = s.client.Close()
		s.client = nil
		return false
	}
	return true
}

func (s *Server) reply(conn net.Conn, resp response) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	_ = writeFrame(conn, frame{Type: TResp, Payload: marshal(resp)})
}

func (s *Server) session(id int64) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) kill(target *session) {
	s.mu.Lock()
	running := target.running
	s.mu.Unlock()
	if running && target.cmd.Process != nil {
		_ = target.cmd.Process.Kill()
	}
	_ = target.ptmx.Close()
	s.mu.Lock()
	delete(s.sessions, target.id)
	s.mu.Unlock()
}

// shutdown kills every session and stops the server.
func (s *Server) shutdown() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.sessions))
	for _, current := range s.sessions {
		all = append(all, current)
	}
	s.mu.Unlock()
	for _, current := range all {
		s.kill(current)
	}
	close(s.done)
}

// foreground resolves the session's foreground process name, cached.
func (t *session) foreground() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return ""
	}
	if time.Since(t.fgCheckedAt) < 2*time.Second {
		return t.fgName
	}
	t.fgCheckedAt = time.Now()
	name := ""
	if pgid, err := unix.IoctlGetInt(int(t.ptmx.Fd()), unix.TIOCGPGRP); err == nil && pgid > 0 {
		name = processName(pgid)
	}
	t.fgName = name
	return name
}

// processName resolves a pid to its command name (Linux /proc, else ps).
func processName(pid int) string {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		return strings.TrimSpace(string(data))
	}
	output, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(filepath.Base(strings.TrimSpace(string(output))), "-")
}
