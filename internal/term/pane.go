package term

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/patriceckhart/hrdx/internal/vt"
	"golang.org/x/sys/unix"
)

// Pane is one real terminal session: a subprocess on a PTY whose output is
// parsed into a virtual screen that the TUI renders as ANSI text.
type Pane struct {
	mu        sync.Mutex
	vt        vt.Terminal
	pty       *os.File
	cmd       *exec.Cmd
	updates   chan struct{}
	exited    bool
	kittyKeys bool
	scanTail  []byte

	// Foreground process cache for ForegroundCommand.
	fgName      string
	fgCheckedAt time.Time

	// scrollOffset counts lines scrolled back into history; 0 is live.
	scrollOffset int

	// selection, in stable line coordinates: line 0 is the oldest history
	// line, history length + screen row addresses the visible screen.
	selecting bool
	selStartX int
	selStartL int
	selEndX   int
	selEndL   int
	selActive bool
}

// Start launches command in cwd on a fresh PTY sized cols x rows.
func Start(command string, args []string, cwd string, cols, rows int) (*Pane, error) {
	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	pane := &Pane{
		vt:      vt.New(vt.WithSize(cols, rows), vt.WithWriter(ptmx)),
		pty:     ptmx,
		cmd:     cmd,
		updates: make(chan struct{}, 1),
	}
	go pane.reader()
	return pane, nil
}

// Updates delivers a signal whenever the screen content changed. The channel
// closes when the subprocess exits.
func (p *Pane) Updates() <-chan struct{} { return p.updates }

func (p *Pane) reader() {
	buffer := make([]byte, 32*1024)
	for {
		n, err := p.pty.Read(buffer)
		if n > 0 {
			p.mu.Lock()
			p.scanKeyboardProtocol(buffer[:n])
			_, _ = p.vt.Write(buffer[:n])
			p.mu.Unlock()
			p.notify()
		}
		if err != nil {
			break
		}
	}
	_ = p.cmd.Wait()
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
	close(p.updates)
}

// scanKeyboardProtocol watches the output stream for kitty keyboard
// protocol pushes and pops so the multiplexer knows whether this child
// understands CSI-u key encodings. Called with p.mu held.
func (p *Pane) scanKeyboardProtocol(chunk []byte) {
	data := append(p.scanTail, chunk...)
	if bytes.Contains(data, []byte("\x1b[>1u")) || bytes.Contains(data, []byte("\x1b[>4;2m")) {
		p.kittyKeys = true
	}
	if bytes.Contains(data, []byte("\x1b[<u")) {
		p.kittyKeys = false
	}
	// Keep a short tail so sequences split across reads are still seen.
	if len(data) > 8 {
		data = data[len(data)-8:]
	}
	p.scanTail = append(p.scanTail[:0], data...)
}

// fgCacheTTL bounds how often ForegroundCommand does the ioctl + process
// name lookup; the sidebar queries it on every render.
const fgCacheTTL = 2 * time.Second

// ForegroundCommand returns the name of the foreground process on the
// pane's PTY (e.g. "zsh" for an idle shell, "zot" while zot runs in it).
// The result is cached briefly; "" when it cannot be determined.
func (p *Pane) ForegroundCommand() string {
	p.mu.Lock()
	if p.exited {
		p.mu.Unlock()
		return ""
	}
	if time.Since(p.fgCheckedAt) < fgCacheTTL {
		name := p.fgName
		p.mu.Unlock()
		return name
	}
	p.fgCheckedAt = time.Now()
	fd := int(p.pty.Fd())
	p.mu.Unlock()

	// The PTY master reports the foreground process group of its slave
	// side; the group leader is the running command.
	name := ""
	if pgid, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP); err == nil && pgid > 0 {
		name = processName(pgid)
	}

	p.mu.Lock()
	p.fgName = name
	p.mu.Unlock()
	return name
}

// processName resolves a pid to its command name. Linux reads /proc, other
// unixes shell out to ps; both paths return the bare name without path.
func processName(pid int) string {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		return strings.TrimSpace(string(data))
	}
	output, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(output))
	// Login shells report as "-zsh"; strip the marker before basing.
	return strings.TrimPrefix(filepath.Base(name), "-")
}

// KittyKeys reports whether the child requested enhanced (CSI-u) keyboard
// input, so chords like ctrl+1 can be forwarded in their native encoding.
func (p *Pane) KittyKeys() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.kittyKeys
}

// HasSpinner reports whether the visible screen contains a braille spinner
// glyph (U+2800..U+28FF). zot's TUI shows one while a turn is running, so
// this is screen-evidence that the agent is working rather than idle.
func (p *Pane) HasSpinner() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return false
	}
	p.vt.Lock()
	defer p.vt.Unlock()
	cols, rows := p.vt.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			char := p.vt.Cell(x, y).Char
			if char >= 0x2801 && char <= 0x28ff {
				return true
			}
		}
	}
	return false
}

// HasScreenText reports whether the visible screen contains the given
// substring, ignoring styling. Rows are matched individually with runs of
// nulls/spaces collapsed, so cell padding does not break the match.
func (p *Pane) HasScreenText(needle string) bool {
	if needle == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return false
	}
	p.vt.Lock()
	defer p.vt.Unlock()
	cols, rows := p.vt.Size()
	var row bytes.Buffer
	for y := 0; y < rows; y++ {
		row.Reset()
		lastSpace := false
		for x := 0; x < cols; x++ {
			char := p.vt.Cell(x, y).Char
			if char == 0 {
				char = ' '
			}
			if char == ' ' {
				if lastSpace {
					continue
				}
				lastSpace = true
			} else {
				lastSpace = false
			}
			row.WriteRune(char)
		}
		if strings.Contains(row.String(), needle) {
			return true
		}
	}
	return false
}

func (p *Pane) notify() {
	select {
	case p.updates <- struct{}{}:
	default:
	}
}

// Write forwards raw input bytes (key encodings) to the subprocess and
// snaps the view back to live output.
func (p *Pane) Write(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scrollOffset = 0
	if p.exited {
		return
	}
	_, _ = p.pty.Write(data)
}

// Resize grows or shrinks both the PTY and the virtual screen.
func (p *Pane) Resize(cols, rows int) {
	if cols < 2 || rows < 2 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return
	}
	p.vt.Resize(cols, rows)
	_ = pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Running reports whether the subprocess is still alive.
func (p *Pane) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.exited
}

// Title returns the OSC window title set by the subprocess, if any.
func (p *Pane) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Title()
}

// AppCursor reports whether the subprocess enabled application cursor keys.
func (p *Pane) AppCursor() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Mode()&vt.ModeAppCursor != 0
}

// MouseEnabled reports whether the subprocess asked for mouse reporting.
func (p *Pane) MouseEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.vt.Mode()&vt.ModeMouseMask != 0
}

// AltScreen reports whether the child runs a full-screen app.
func (p *Pane) AltScreen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	defer p.vt.Unlock()
	return p.vt.AltScreen()
}

// Scroll moves the view into history (positive delta) or back toward live
// output (negative delta). Returns true when the offset changed.
func (p *Pane) Scroll(delta int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.vt.Lock()
	limit := p.vt.HistoryLen()
	p.vt.Unlock()
	next := p.scrollOffset + delta
	if next < 0 {
		next = 0
	}
	if next > limit {
		next = limit
	}
	changed := next != p.scrollOffset
	p.scrollOffset = next
	return changed
}

// ScrollOffset returns the current scrollback offset in lines.
func (p *Pane) ScrollOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scrollOffset
}

// ResetScroll snaps back to live output.
func (p *Pane) ResetScroll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scrollOffset = 0
}

// StartSelection begins a text selection at visible cell (x, y).
func (p *Pane) StartSelection(x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	line := p.lineForRow(y)
	p.selecting = true
	p.selActive = true
	p.selStartX, p.selStartL = x, line
	p.selEndX, p.selEndL = x, line
}

// ExtendSelection updates the selection end while dragging.
func (p *Pane) ExtendSelection(x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.selecting {
		return
	}
	p.selEndX, p.selEndL = x, p.lineForRow(y)
}

// FinishSelection ends the drag, keeping the highlight.
func (p *Pane) FinishSelection() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selecting = false
}

// ClearSelection removes the highlight.
func (p *Pane) ClearSelection() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selecting = false
	p.selActive = false
}

// HasSelection reports whether a selection highlight exists.
func (p *Pane) HasSelection() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.selActive
}

// lineForRow converts a visible row into a stable line index taking the
// scroll offset into account. Called with p.mu held.
func (p *Pane) lineForRow(y int) int {
	p.vt.Lock()
	defer p.vt.Unlock()
	return p.vt.HistoryLen() - p.scrollOffset + y
}

// SelectionText returns the selected text with trailing spaces trimmed per
// line and newlines between lines.
func (p *Pane) SelectionText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.selActive {
		return ""
	}
	p.vt.Lock()
	defer p.vt.Unlock()

	startL, startX, endL, endX := p.orderedSelection()
	cols, _ := p.vt.Size()

	var out bytes.Buffer
	for line := startL; line <= endL; line++ {
		fromX, toX := 0, cols-1
		if line == startL {
			fromX = startX
		}
		if line == endL {
			toX = endX
		}
		text := p.lineText(line, fromX, toX)
		out.WriteString(text)
		if line != endL {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// lineText extracts trimmed text from one stable line. Lock held.
func (p *Pane) lineText(line, fromX, toX int) string {
	cols, rows := p.vt.Size()
	historyLen := p.vt.HistoryLen()
	var runes []rune
	if line < historyLen {
		glyphs := p.vt.HistoryLine(line)
		for x := fromX; x <= toX && x < len(glyphs); x++ {
			runes = append(runes, glyphChar(glyphs[x].Char))
		}
	} else {
		row := line - historyLen
		if row < 0 || row >= rows {
			return ""
		}
		for x := fromX; x <= toX && x < cols; x++ {
			runes = append(runes, glyphChar(p.vt.Cell(x, row).Char))
		}
	}
	end := len(runes)
	for end > 0 && runes[end-1] == ' ' {
		end--
	}
	return string(runes[:end])
}

func glyphChar(char rune) rune {
	if char == 0 {
		return ' '
	}
	return char
}

// orderedSelection normalizes the selection so start <= end. Lock held.
func (p *Pane) orderedSelection() (startL, startX, endL, endX int) {
	startL, startX = p.selStartL, p.selStartX
	endL, endX = p.selEndL, p.selEndX
	if startL > endL || (startL == endL && startX > endX) {
		startL, endL = endL, startL
		startX, endX = endX, startX
	}
	return startL, startX, endL, endX
}

// Close terminates the subprocess and releases the PTY.
func (p *Pane) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.pty.Close()
}
