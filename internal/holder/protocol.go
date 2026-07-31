// Package holder implements the session holder: a small background
// process that owns every pane's PTY so shells and agents survive TUI
// restarts. The TUI connects over a unix socket, streams pane I/O
// through it, and detaches on quit; the holder keeps reading each PTY
// into a bounded ring buffer that is replayed on the next attach.
package holder

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Protocol is bumped on incompatible wire changes. A client that meets
// a holder speaking a different protocol shuts it down and spawns a
// fresh one (running sessions are lost, which beats undefined behavior).
const Protocol = 1

// Frame types. Control frames carry JSON, data frames raw bytes.
const (
	TCtrl byte = 1 // client -> holder request (JSON)
	TResp byte = 2 // holder -> client response (JSON)
	TEvt  byte = 3 // holder -> client unsolicited event (JSON)
	TOut  byte = 4 // holder -> client pane output (raw, ID = session)
	TIn   byte = 5 // client -> holder pane input (raw, ID = session)
)

// maxFrame guards against corrupt length prefixes.
const maxFrame = 32 << 20

// frame is one length-prefixed message on the wire:
// uint32 payload length, uint8 type, int64 id, payload.
type frame struct {
	Type    byte
	ID      int64
	Payload []byte
}

func writeFrame(w io.Writer, f frame) error {
	head := make([]byte, 13)
	binary.BigEndian.PutUint32(head[0:4], uint32(len(f.Payload)))
	head[4] = f.Type
	binary.BigEndian.PutUint64(head[5:13], uint64(f.ID))
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(f.Payload)
	return err
}

func readFrame(r io.Reader) (frame, error) {
	head := make([]byte, 13)
	if _, err := io.ReadFull(r, head); err != nil {
		return frame{}, err
	}
	length := binary.BigEndian.Uint32(head[0:4])
	if length > maxFrame {
		return frame{}, fmt.Errorf("frame too large: %d", length)
	}
	f := frame{
		Type: head[4],
		ID:   int64(binary.BigEndian.Uint64(head[5:13])),
	}
	if length > 0 {
		f.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return frame{}, err
		}
	}
	return f, nil
}

// request is the JSON payload of a TCtrl frame. Req correlates the
// response; Op selects the operation.
type request struct {
	Req int64  `json:"req"`
	Op  string `json:"op"` // hello, start, attach, resize, kill, fg, list, shutdown

	// hello
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`

	// start
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	CWD     string   `json:"cwd,omitempty"`
	Env     []string `json:"env,omitempty"`

	// start, attach, resize
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// attach, resize, kill, fg (also carried in the frame ID)
	Session int64 `json:"session,omitempty"`
}

// response is the JSON payload of a TResp frame.
type response struct {
	Req int64  `json:"req"`
	Err string `json:"err,omitempty"`

	// hello
	Version  string `json:"version,omitempty"`
	Protocol int    `json:"protocol,omitempty"`

	// start, attach
	Session int64 `json:"session,omitempty"`
	Running bool  `json:"running,omitempty"`

	// fg
	Name string `json:"name,omitempty"`

	// list
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

// event is the JSON payload of a TEvt frame.
type event struct {
	Event   string `json:"event"` // "exited"
	Session int64  `json:"session"`
}

// SessionInfo describes one holder session in a list response.
type SessionInfo struct {
	ID      int64  `json:"id"`
	Command string `json:"command"`
	CWD     string `json:"cwd"`
	Running bool   `json:"running"`
}

func marshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
