package termimg

import (
	"bytes"
	"os"
	"strconv"
	"sync"
)

// Placement is one image pinned to a cell rectangle. X and Y are 0-based
// screen cells. Key identifies the content: a placement whose key and
// rectangle match what is on screen is not redrawn.
type Placement struct {
	Key        string
	X, Y, W, H int
	Data       []byte // complete Sixel sequence sized for W x H cells
}

func (p Placement) sameSpot(q Placement) bool {
	return p.Key == q.Key && p.X == q.X && p.Y == q.Y && p.W == q.W && p.H == q.H
}

// Layer holds the images that should be on screen and what was last drawn.
// The UI publishes placements every frame with Set; the Writer applies the
// difference around Bubbletea's text for that frame.
//
// The cells under a placement must render as constant blanks in the text
// frame. Bubbletea only rewrites cells that change, so constant blanks keep
// the pixels intact between redraws.
type Layer struct {
	mu    sync.Mutex
	want  []Placement
	drawn []Placement
	full  bool // redraw everything: the screen was cleared
}

// NewLayer returns an empty layer.
func NewLayer() *Layer { return &Layer{} }

// Set replaces the desired placements. It is cheap and safe to call from
// View on every frame; nil clears every image.
func (l *Layer) Set(ps []Placement) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.want = append(l.want[:0], ps...)
	l.mu.Unlock()
}

// Invalidate forces a full redraw on the next frame (after a resize or
// anything else that repaints the screen).
func (l *Layer) Invalidate() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.full = true
	l.mu.Unlock()
}

// render returns the bytes that bring the screen from drawn to want. pre
// erases placements that went away or moved; it runs before the frame's
// text so it cannot wipe text that now occupies those cells. post draws new
// or changed images after the text, so the text cannot overwrite them.
func (l *Layer) render(full bool) (pre, post []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	full = full || l.full
	l.full = false
	var e, d bytes.Buffer
	if !full {
		for _, old := range l.drawn {
			if !containsSpot(l.want, old) {
				erase(&e, old)
			}
		}
	}
	for _, p := range l.want {
		if len(p.Data) == 0 || (!full && containsSpot(l.drawn, p)) {
			continue
		}
		cup(&d, p.Y, p.X)
		d.Write(p.Data)
	}
	l.drawn = append(l.drawn[:0], l.want...)
	return e.Bytes(), d.Bytes()
}

func containsSpot(ps []Placement, p Placement) bool {
	for _, q := range ps {
		if q.sameSpot(p) {
			return true
		}
	}
	return false
}

// erase clears a rectangle's cells (ECH), which removes image pixels too.
func erase(b *bytes.Buffer, p Placement) {
	for r := range p.H {
		cup(b, p.Y+r, p.X)
		b.WriteString("\x1b[")
		b.WriteString(strconv.Itoa(p.W))
		b.WriteByte('X')
	}
}

func cup(b *bytes.Buffer, row, col int) {
	b.WriteString("\x1b[")
	b.WriteString(strconv.Itoa(row + 1))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(col + 1))
	b.WriteByte('H')
}

var (
	syncBegin = []byte("\x1b[?2026h")
	syncEnd   = []byte("\x1b[?2026l")
	saveCur   = []byte("\x1b7")
	restCur   = []byte("\x1b8")
	// Sequences Bubbletea emits when it repaints from scratch; any of them
	// wipes images, so the layer redraws everything after that frame.
	clearSeqs = [][]byte{[]byte("\x1b[2J"), []byte("\x1b[J"), []byte("\x1b[0J")}
)

// Writer is the program output. It passes Bubbletea's bytes through and,
// on each frame, wraps the frame's text with the layer's image updates
// inside the same synchronized update, saving and restoring the cursor
// around them. It embeds *os.File so Bubbletea still sees a terminal.
type Writer struct {
	*os.File
	layer *Layer
	mu    sync.Mutex
}

// NewWriter wraps f (normally os.Stdout) for layer.
func NewWriter(f *os.File, layer *Layer) *Writer {
	return &Writer{File: f, layer: layer}
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// A frame ends its synchronized update; without mode 2026 any sizable
	// write is the best available frame boundary.
	if !bytes.HasSuffix(p, syncEnd) && len(p) <= 512 {
		return w.File.Write(p)
	}
	full := false
	for _, s := range clearSeqs {
		if bytes.Contains(p, s) {
			full = true
			break
		}
	}
	pre, post := w.layer.render(full)
	if len(pre) == 0 && len(post) == 0 {
		return w.File.Write(p)
	}
	head, body, tail := []byte(nil), p, []byte(nil)
	if bytes.HasPrefix(body, syncBegin) {
		head, body = syncBegin, body[len(syncBegin):]
	}
	if bytes.HasSuffix(body, syncEnd) {
		body, tail = body[:len(body)-len(syncEnd)], syncEnd
	}
	var b bytes.Buffer
	b.Grow(len(p) + len(pre) + len(post) + 16)
	b.Write(head)
	wrap(&b, pre)
	b.Write(body)
	wrap(&b, post)
	b.Write(tail)
	if _, err := w.File.Write(b.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

func wrap(b *bytes.Buffer, seq []byte) {
	if len(seq) == 0 {
		return
	}
	b.Write(saveCur)
	b.Write(seq)
	b.Write(restCur)
}
