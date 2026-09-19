package plugin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

const MaxFrameBytes = 256 << 10

// MaxInputBytes bounds one peer pane.send_text payload. Larger prompts should
// be written to a file the agent reads, not typed.
const MaxInputBytes = 16 << 10

// Frame is the independent experimental stdio protocol. IDs are directional:
// host and peer calls may use the same ID without sharing pending-call maps.
type Frame struct {
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	Plugin   string          `json:"plugin,omitempty"`
	Protocol int             `json:"protocol,omitempty"`
	Instance string          `json:"instance,omitempty"`
	Grants   []string        `json:"grants,omitempty"`
	Config   map[string]any  `json:"config,omitempty"`
	Method   string          `json:"method,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *Problem        `json:"error,omitempty"`
}

func readFrames(reader io.Reader, deliver func(Frame) bool) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes+1)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			return index + 1, bytes.TrimSuffix(data[:index], []byte{'\r'}), nil
		}
		if atEOF && len(data) > 0 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || len(line) > MaxFrameBytes || !utf8.Valid(line) || !uniqueJSON(line) {
			return problem("invalid_frame", "invalid or oversized protocol frame")
		}
		var frame Frame
		if err := json.Unmarshal(line, &frame); err != nil || frame.Type == "" || len(frame.ID) > 128 || !plainText(frame.ID, 128, false) {
			return problem("invalid_frame", "invalid protocol frame fields")
		}
		if (frame.Type == "request" && (frame.ID == "" || frame.Method == "")) || ((frame.Type == "response" || frame.Type == "cancel") && frame.ID == "") {
			return problem("invalid_frame", "request, response, and cancel frames require IDs")
		}
		if !deliver(frame) {
			return nil
		}
	}
	if scanner.Err() != nil {
		return problem("invalid_frame", "cannot read protocol frame")
	}
	return io.EOF
}

func encodeFrame(frame Frame) ([]byte, error) {
	data, err := json.Marshal(frame)
	if err != nil || len(data) > MaxFrameBytes {
		return nil, problem("too_large", "protocol frame exceeds 256 KiB")
	}
	return append(data, '\n'), nil
}
