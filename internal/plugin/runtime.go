package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/patriceckhart/hrdx/internal/state"
)

const (
	HandshakeTimeout = 5 * time.Second
	CallTimeout      = 10 * time.Second
	ShutdownTimeout  = 500 * time.Millisecond
	ProviderTimeout  = 2 * time.Second
	MaxCalls         = 32
)

// ErrInvalidResult marks a peer response that does not match the schema.
var ErrInvalidResult = problem("invalid_result", "plugin returned an invalid result")

type Registration struct {
	Package  Package
	Approval state.PluginApproval
}

type Status struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Generation string `json:"generation,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Reply struct {
	Result any
	Error  *Problem
}

// Request carries trusted transport identity and a deadline into the UI loop.
// The peer cannot choose Plugin or Generation. Reply must be buffered.
type Request struct {
	Plugin     string
	Generation string
	Context    context.Context
	Method     string
	Params     json.RawMessage
	Reply      chan Reply
}

type Event struct {
	Status  *Status
	Request *Request
}

// Runtime owns processes only. It never touches UI, PTYs, or workspace state.
// Events are bounded and consumed by a Bubble Tea wait command.
type Runtime struct {
	mu           sync.Mutex
	entries      map[string]Registration
	sessions     map[string]*session
	statuses     map[string]Status
	events       chan Event
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	closing      bool
	store        *Store
	approvalBase string
	watchCancel  context.CancelFunc
}

func NewRuntime(registrations []Registration, dataDirectory ...string) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{entries: make(map[string]Registration), sessions: make(map[string]*session), statuses: make(map[string]Status), events: make(chan Event, 64), ctx: ctx, cancel: cancel}
	if len(dataDirectory) > 0 && dataDirectory[0] != "" {
		r.store = &Store{Directory: dataDirectory[0]}
	}
	for _, registration := range registrations {
		id := registration.Package.ID
		if !validID(id) {
			continue
		}
		if _, duplicate := r.entries[id]; duplicate {
			delete(r.entries, id)
			r.statuses[id] = Status{ID: id, State: "blocked", Error: "duplicate plugin ID"}
			continue
		}
		if r.statuses[id].State == "blocked" {
			continue
		}
		if !registration.Package.Valid || registration.Package.Manifest == nil || registration.Package.Manifest.ID != id || registration.Approval.ID != id {
			r.statuses[id] = Status{ID: id, State: "blocked", Error: "invalid plugin registration"}
			continue
		}
		r.entries[id] = registration
		r.statuses[id] = Status{ID: id, State: "approved"}
	}
	return r
}

// Publish is best effort and never blocks the UI. Missing sequence numbers
// tell a peer to refresh its scoped snapshot after backpressure.
func (r *Runtime) Publish(id, generation, method string, params any) bool {
	r.mu.Lock()
	s := r.sessions[id]
	status := r.statuses[id]
	r.mu.Unlock()
	if s == nil || status.State != "ready" || status.Generation != generation || s.stopping() {
		return false
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return false
	}
	data, err := encodeFrame(Frame{Type: "event", Method: method, Params: payload})
	if err != nil {
		return false
	}
	select {
	case <-s.ctx.Done():
		return false
	case s.outgoing <- data:
		return true
	default:
		return false
	}
}

func (r *Runtime) Events() <-chan Event  { return r.events }
func (r *Runtime) Done() <-chan struct{} { return r.ctx.Done() }

func (r *Runtime) Statuses() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := make([]Status, 0, len(r.statuses))
	for _, status := range r.statuses {
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

func (r *Runtime) Registrations() []Registration {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]Registration, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Package.ID < entries[j].Package.ID })
	return entries
}

// Authorize rechecks session liveness and grants immediately before a UI
// operation. Context cancellation prevents timed-out queued mutations.
func (r *Runtime) Authorize(request Request, grant string) (state.PluginApproval, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[request.Plugin]
	status := r.statuses[request.Plugin]
	s := r.sessions[request.Plugin]
	return entry.Approval, !r.closing && ok && s != nil && !s.stopping() && s.ctx.Err() == nil && request.Context != nil && request.Context.Err() == nil && status.State == "ready" && status.Generation == request.Generation && HasGrant(entry.Approval, grant)
}

func (r *Runtime) Start(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startLocked(id, true)
}

func (r *Runtime) startLocked(id string, explicit bool) error {
	if r.closing || r.ctx.Err() != nil {
		return problem("stopped", "plugin runtime stopped")
	}
	entry, ok := r.entries[id]
	if !ok {
		return problem("not_found", "plugin is not registered")
	}
	if !entry.Approval.Enabled {
		return problem("not_approved", "plugin approval was revoked")
	}
	status := r.statuses[id].State
	if !explicit && status != "approved" && status != "ready" && status != "starting" && status != "handshaking" {
		return problem("unavailable", "plugin requires an explicit restart")
	}
	if existing := r.sessions[id]; existing != nil {
		if !explicit && (existing.stopping() || existing.ctx.Err() != nil) {
			return problem("unavailable", "plugin disconnected")
		}
		select {
		case <-existing.done:
		default:
			return nil
		}
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return problem("error", "cannot create plugin generation")
	}
	ctx, cancel := context.WithCancel(r.ctx)
	s := &session{runtime: r, entry: entry, generation: hex.EncodeToString(token[:]), ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), stop: make(chan struct{}), outgoing: make(chan []byte, 32), calls: make(map[string]chan Frame), incoming: make(map[string]context.CancelFunc)}
	r.sessions[id] = s
	r.statuses[id] = Status{ID: id, State: "starting", Generation: s.generation}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		s.run()
	}()
	return nil
}

func (r *Runtime) Stop(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.sessions[id]; s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
	}
	status := r.statuses[id]
	if status.ID != "" && r.entries[id].Approval.Enabled {
		status.State = "stopped"
		r.statuses[id] = status
	}
}

func (r *Runtime) Restart(ctx context.Context, id string) error {
	r.Stop(id)
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	if s != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return problem("timeout", "plugin restart timed out")
		}
	}
	return r.Start(id)
}

// Reload replaces a registration from an already saved approval, then starts a
// new generation. It cannot approve code, change package identity, or move the
// package. The management CLI must explicitly reapprove changed files first.
func (r *Runtime) Reload(ctx context.Context, id string) error {
	r.mu.Lock()
	entry, ok := r.entries[id]
	base := r.approvalBase
	r.mu.Unlock()
	if !ok || base == "" {
		return problem("not_found", "plugin is not registered for live reload")
	}
	approval, err := state.LoadPluginApproval(ApprovalPath(base, id))
	if err != nil || approval.ID != id || approval.Path != entry.Package.Path {
		return problem("not_approved", "reload requires a current approval for the same package path")
	}
	if approval.Instance != entry.Approval.Instance || !slices.Equal(approval.Grants, entry.Approval.Grants) || !slices.Equal(approval.Workspaces, entry.Approval.Workspaces) {
		return problem("not_approved", "reload cannot change grants or scopes, restart hrdx to load approval changes")
	}
	refreshed := InspectPackage(approval.Path)
	if err := VerifyApproval(refreshed, approval); err != nil {
		return err
	}
	r.Stop(id)
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	if s != nil {
		select {
		case <-s.done:
		case <-ctx.Done():
			return problem("timeout", "plugin reload timed out")
		}
	}
	r.mu.Lock()
	if r.closing || r.ctx.Err() != nil {
		r.mu.Unlock()
		return problem("stopped", "plugin runtime stopped")
	}
	r.entries[id] = Registration{Package: refreshed, Approval: approval}
	r.statuses[id] = Status{ID: id, State: "approved"}
	err = r.startLocked(id, true)
	r.mu.Unlock()
	return err
}

func (r *Runtime) BeginShutdown() {
	r.mu.Lock()
	r.closing = true
	if r.watchCancel != nil {
		r.watchCancel()
	}
	for _, s := range r.sessions {
		s.stopOnce.Do(func() { close(s.stop) })
	}
	r.mu.Unlock()
}

func (r *Runtime) Close() {
	r.BeginShutdown()
	r.wg.Wait()
	r.cancel()
}

func (r *Runtime) Call(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	result, _, err := r.CallTracked(ctx, id, method, params)
	return result, err
}

// Activate starts a lazily activated plugin and waits for its ready frame. The
// returned generation lets callers revalidate asynchronous targets before
// invoking work on that exact connection.
func (r *Runtime) Activate(ctx context.Context, id string) (string, error) {
	if ctx.Err() != nil {
		return "", problem("canceled", "plugin activation canceled")
	}
	r.mu.Lock()
	err := r.startLocked(id, false)
	s := r.sessions[id]
	r.mu.Unlock()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	select {
	case <-s.ready:
	case <-s.done:
		return s.generation, problem("unavailable", "plugin did not become ready")
	case <-ctx.Done():
		return s.generation, problem("timeout", "plugin activation timed out")
	}
	if ctx.Err() != nil || s.stopping() {
		return s.generation, problem("canceled", "plugin activation canceled")
	}
	return s.generation, nil
}

// CallGeneration invokes only the specified live generation. It never starts
// or redirects work to a replacement process.
func (r *Runtime) CallGeneration(ctx context.Context, id, generation, method string, params any) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return nil, problem("canceled", "plugin call canceled")
	}
	r.mu.Lock()
	s := r.sessions[id]
	status := r.statuses[id]
	r.mu.Unlock()
	if s == nil || s.generation != generation || status.State != "ready" || status.Generation != generation || s.stopping() || s.ctx.Err() != nil {
		return nil, problem("unavailable", "plugin generation is no longer ready")
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	return s.call(ctx, method, params)
}

// CallTracked tags results with their connection generation so the UI can
// discard late command results after a stop, restart, or approval reload.
func (r *Runtime) CallTracked(ctx context.Context, id, method string, params any) (json.RawMessage, string, error) {
	generation, err := r.Activate(ctx, id)
	if err != nil {
		return nil, generation, err
	}
	result, err := r.CallGeneration(ctx, id, generation, method, params)
	return result, generation, err
}

func (s *session) transition(lifecycle, message string) {
	status := Status{ID: s.entry.Package.ID, State: lifecycle, Generation: s.generation, Error: message}
	s.runtime.mu.Lock()
	if s.runtime.sessions[status.ID] == s {
		if !s.runtime.entries[status.ID].Approval.Enabled {
			status.State, status.Error = "blocked", "approval changed or revoked, restart hrdx to load current approvals"
		}
		s.runtime.statuses[status.ID] = status
	}
	s.runtime.mu.Unlock()
	select {
	case s.runtime.events <- Event{Status: &status}:
	default: // Statuses() is authoritative if a slow UI misses a notice.
	}
}

type session struct {
	runtime    *Runtime
	entry      Registration
	generation string
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	ready      chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once
	outgoing   chan []byte
	mu         sync.Mutex
	calls      map[string]chan Frame
	incoming   map[string]context.CancelFunc
	next       uint64
	workers    sync.WaitGroup
	failure    string
}

func (s *session) fail(message string) {
	s.mu.Lock()
	if s.failure == "" {
		s.failure = message
	}
	s.mu.Unlock()
	s.cancel()
}

func (s *session) stopping() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func (s *session) send(frame Frame) bool {
	if s.ctx.Err() != nil {
		return false
	}
	data, err := encodeFrame(frame)
	if err != nil {
		return false
	}
	select {
	case <-s.ctx.Done():
		return false
	case s.outgoing <- data:
		return true
	default:
		s.fail("plugin output queue is full")
		return false
	}
}

func (s *session) run() {
	defer close(s.done)
	defer s.cancel()
	verified := InspectPackage(s.entry.Package.Path)
	if err := VerifyApproval(verified, s.entry.Approval); err != nil {
		s.transition("blocked", err.Error())
		return
	}
	if s.stopping() || s.ctx.Err() != nil {
		s.transition("stopped", "")
		return
	}
	manifest := verified.Manifest
	command := exec.Command(filepath.Join(s.entry.Package.Path, filepath.FromSlash(manifest.Entrypoint)), manifest.Args...)
	command.Dir = s.entry.Package.Path
	command.Env = append(os.Environ(), "HRDX_PLUGIN_ID="+manifest.ID, "HRDX_PLUGIN_INSTANCE="+s.generation)
	// Do not persist stderr, argv, environment, or protocol payloads. Trusted
	// plugins may contain secrets in all of them. Discard drains without logs.
	command.Stderr = io.Discard
	command.WaitDelay = ShutdownTimeout
	stdin, err := command.StdinPipe()
	if err != nil {
		s.transition("failed", "cannot create plugin input")
		return
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stdin.Close()
		s.transition("failed", "cannot create plugin output")
		return
	}
	kill, err := startProcess(command)
	if err != nil {
		stdin.Close()
		stdout.Close()
		s.transition("failed", "cannot start plugin process")
		return
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	frames := make(chan Frame, 8)
	readError := make(chan error, 1)
	ioDone := make(chan struct{})
	go func() {
		defer close(ioDone)
		readError <- readFrames(stdout, func(frame Frame) bool {
			select {
			case frames <- frame:
				return true
			case <-s.ctx.Done():
				return false
			}
		})
	}()
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for {
			select {
			case data := <-s.outgoing:
				if _, err := stdin.Write(data); err != nil {
					s.fail("cannot write plugin input")
					return
				}
			case <-s.ctx.Done():
				return
			}
		}
	}()
	processExited := false
	defer func() {
		s.cancel()
		kill()
		stdin.Close()
		stdout.Close()
		if !processExited {
			<-exited
		}
		<-ioDone
		<-writeDone
		s.workers.Wait()
		// Retain lifecycle diagnostics, not megabytes of queued peer payloads.
		for len(s.outgoing) > 0 {
			<-s.outgoing
		}
	}()
	s.transition("handshaking", "")
	timer := time.NewTimer(HandshakeTimeout)
	defer timer.Stop()
	stage := 0
	windowStart, frameCount := time.Now(), 0
	stop := s.stop
	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			failure := s.failure
			s.mu.Unlock()
			if failure != "" && !s.stopping() {
				s.transition("failed", failure)
			} else {
				s.transition("stopped", "")
			}
			return
		case <-stop:
			stop = nil
			stage = 3
			s.transition("stopping", "")
			s.send(Frame{Type: "shutdown"})
			timer.Reset(ShutdownTimeout)
		case <-exited:
			processExited = true
			if s.stopping() {
				s.transition("stopped", "")
			} else {
				s.transition("failed", "plugin process exited")
			}
			return
		case <-readError:
			if s.stopping() {
				s.transition("stopped", "")
			} else {
				s.transition("failed", "plugin output closed or contained an invalid frame")
			}
			return
		case <-timer.C:
			if stage == 3 {
				s.transition("stopped", "")
			} else {
				s.transition("failed", "plugin handshake timed out")
			}
			return
		case frame := <-frames:
			if time.Since(windowStart) >= time.Second {
				windowStart, frameCount = time.Now(), 0
			}
			frameCount++
			if frameCount > 256 {
				s.transition("failed", "plugin exceeded 256 frames per second")
				return
			}
			if stage == 3 {
				if frame.Type == "shutdown_ack" {
					s.transition("stopped", "")
					return
				}
				continue
			}
			if stage == 0 {
				if frame.Type != "hello" || frame.Plugin != manifest.ID || frame.Protocol != ProtocolVersion {
					s.transition("failed", "invalid plugin hello")
					return
				}
				s.send(Frame{Type: "hello_ack", Protocol: ProtocolVersion, Instance: s.generation, Grants: s.entry.Approval.Grants, Config: EffectiveConfig(manifest, s.entry.Approval)})
				stage = 1
				continue
			}
			if stage == 1 {
				if frame.Type != "ready" {
					s.transition("failed", "expected plugin ready")
					return
				}
				timer.Stop()
				stage = 2
				s.transition("ready", "")
				close(s.ready)
				continue
			}
			switch frame.Type {
			case "response":
				s.mu.Lock()
				if reply := s.calls[frame.ID]; reply != nil {
					select {
					case reply <- frame:
					default:
					}
				}
				s.mu.Unlock()
			case "request":
				s.hostRequest(frame)
			case "cancel":
				s.mu.Lock()
				if cancel := s.incoming[frame.ID]; cancel != nil {
					cancel()
				}
				s.mu.Unlock()
			default:
				s.transition("failed", "unexpected runtime frame")
				return
			}
		}
	}
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, problem("invalid_params", "cannot encode plugin call")
	}
	s.mu.Lock()
	if len(s.calls) >= MaxCalls {
		s.mu.Unlock()
		return nil, problem("busy", "too many outstanding plugin calls")
	}
	s.next++
	id := fmt.Sprint(s.next)
	reply := make(chan Frame, 1)
	s.calls[id] = reply
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.calls, id); s.mu.Unlock() }()
	if !s.send(Frame{Type: "request", ID: id, Method: method, Params: payload}) {
		return nil, problem("unavailable", "plugin is unavailable or its output queue is full")
	}
	select {
	case frame := <-reply:
		if frame.Error != nil {
			return nil, problem("plugin_error", "plugin command failed")
		}
		return frame.Result, nil
	case <-ctx.Done():
		s.send(Frame{Type: "cancel", ID: id})
		return nil, problem("timeout", "plugin call canceled or timed out")
	case <-s.ctx.Done():
		return nil, problem("unavailable", "plugin disconnected")
	}
}

func (s *session) hostRequest(frame Frame) {
	s.mu.Lock()
	if frame.ID == "" || len(s.incoming) >= MaxCalls || s.incoming[frame.ID] != nil {
		s.mu.Unlock()
		s.fail("duplicate request ID or too many host requests")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, CallTimeout)
	s.incoming[frame.ID] = cancel
	s.mu.Unlock()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.incoming, frame.ID); s.mu.Unlock() }()
		request := Request{Plugin: s.entry.Package.ID, Generation: s.generation, Context: ctx, Method: frame.Method, Params: frame.Params, Reply: make(chan Reply, 1)}
		var response Reply
		if strings.HasPrefix(frame.Method, "storage.") {
			if _, allowed := s.runtime.Authorize(request, "storage.plugin_private"); !allowed || s.runtime.store == nil {
				response.Error = problem("denied", "plugin storage is not granted or configured")
			} else {
				response = s.runtime.store.Call(ctx, request.Plugin, frame.Method, frame.Params)
			}
		} else {
			select {
			case s.runtime.events <- Event{Request: &request}:
				select {
				case response = <-request.Reply:
				case <-ctx.Done():
					response.Error = problem("timeout", "host call timed out or canceled")
				}
			default:
				response.Error = problem("busy", "host request queue is full")
			}
		}
		payload, err := json.Marshal(response.Result)
		if err != nil {
			response.Error = problem("error", "cannot encode host result")
		}
		if !s.send(Frame{Type: "response", ID: frame.ID, Result: payload, Error: response.Error}) && ctx.Err() == nil {
			s.send(Frame{Type: "response", ID: frame.ID, Error: problem("too_large", "host response exceeds frame limit")})
		}
	}()
}
