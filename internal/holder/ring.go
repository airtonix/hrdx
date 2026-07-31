package holder

// ring is a bounded byte buffer that keeps the newest data. While no
// client is attached, each session's PTY output lands here; on attach
// the content is replayed so the TUI can rebuild the screen.
type ring struct {
	buf   []byte
	start int // index of the oldest byte
	size  int // bytes stored
}

func newRing(capacity int) *ring {
	return &ring{buf: make([]byte, capacity)}
}

// Write appends data, evicting the oldest bytes on overflow.
func (r *ring) Write(data []byte) {
	if len(data) >= len(r.buf) {
		copy(r.buf, data[len(data)-len(r.buf):])
		r.start = 0
		r.size = len(r.buf)
		return
	}
	for _, b := range data {
		index := (r.start + r.size) % len(r.buf)
		r.buf[index] = b
		if r.size < len(r.buf) {
			r.size++
		} else {
			r.start = (r.start + 1) % len(r.buf)
		}
	}
}

// Bytes returns the buffered content, oldest first.
func (r *ring) Bytes() []byte {
	out := make([]byte, r.size)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}
