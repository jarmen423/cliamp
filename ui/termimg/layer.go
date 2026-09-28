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
	return p.Key == q.Key && p.sameRect(q)
}

func (p Placement) sameRect(q Placement) bool {
	return p.X == q.X && p.Y == q.Y && p.W == q.W && p.H == q.H
}

// Layer holds the images that should be on screen and what was last drawn.
// The UI publishes placements every frame with Set; the Writer applies the
// difference around Bubbletea's text for that frame.
//
// The cells under a placement must render as constant blanks in the text
// frame. Bubbletea only rewrites cells that change, so constant blanks keep
// the pixels intact between redraws.
//
// One placement can be live (an animation such as a pixel visualizer): new
// live frames are drawn between Bubbletea frames by the Writer's pump, once
// a frame has established the live rectangle as blank cells.
type Layer struct {
	mu    sync.Mutex
	want  []Placement
	live  *Placement
	drawn []Placement
	full  bool         // redraw everything: the screen was cleared
	dirty map[int]bool // screen rows whose text changed since the last frame
	kick  chan struct{}
}

// NewLayer returns an empty layer.
func NewLayer() *Layer { return &Layer{kick: make(chan struct{}, 1)} }

// SetLive replaces the live placement (nil removes it) and asks the Writer
// to draw it without waiting for Bubbletea's next frame. It is safe to call
// from any goroutine.
func (l *Layer) SetLive(p *Placement) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if p == nil && l.live == nil {
		l.mu.Unlock()
		return
	}
	l.live = p
	l.mu.Unlock()
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// wanted is every placement that should be on screen. Caller holds mu.
func (l *Layer) wanted() []Placement {
	if l.live == nil {
		return l.want
	}
	return append(l.want[:len(l.want):len(l.want)], *l.live)
}

// renderLive draws a new live frame over a live rectangle a Bubbletea frame
// already put on screen. Anything else waits for the next frame.
func (l *Layer) renderLive() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.live == nil || len(l.live.Data) == 0 || l.full {
		return nil
	}
	for i, d := range l.drawn {
		if !d.sameRect(*l.live) {
			continue
		}
		if d.Key == l.live.Key {
			return nil
		}
		var b bytes.Buffer
		cup(&b, l.live.Y, l.live.X)
		b.Write(l.live.Data)
		l.drawn[i] = *l.live
		return b.Bytes()
	}
	return nil
}

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

// Touch marks screen rows whose text changed. Bubbletea rewrites a changed
// line from its first to its last changed cell (or clears to the end of the
// line), which wipes any image in between even where its blank cells did
// not change, so images crossing a touched row are redrawn with the next
// frame. Marks accumulate until a frame is written: Bubbletea may render
// fewer frames than it builds views.
func (l *Layer) Touch(rows ...int) {
	if l == nil || len(rows) == 0 {
		return
	}
	l.mu.Lock()
	if l.dirty == nil {
		l.dirty = map[int]bool{}
	}
	for _, r := range rows {
		l.dirty[r] = true
	}
	l.mu.Unlock()
}

func (l *Layer) touched(p Placement) bool {
	for r := p.Y; r < p.Y+p.H; r++ {
		if l.dirty[r] {
			return true
		}
	}
	return false
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
	want := l.wanted()
	var e, d bytes.Buffer
	if !full {
		for _, old := range l.drawn {
			if !containsRect(want, old) {
				erase(&e, old) // nothing opaque will cover it
			}
		}
	}
	for _, p := range want {
		if len(p.Data) == 0 || (!full && containsSpot(l.drawn, p) && !l.touched(p)) {
			continue
		}
		cup(&d, p.Y, p.X)
		d.Write(p.Data)
	}
	l.drawn = append(l.drawn[:0], want...)
	clear(l.dirty)
	return e.Bytes(), d.Bytes()
}

func containsRect(ps []Placement, p Placement) bool {
	for _, q := range ps {
		if q.sameRect(p) {
			return true
		}
	}
	return false
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

// NewWriter wraps f (normally os.Stdout) for layer and starts the pump that
// draws live frames between Bubbletea frames.
func NewWriter(f *os.File, layer *Layer) *Writer {
	w := &Writer{File: f, layer: layer}
	go w.pump()
	return w
}

func (w *Writer) pump() {
	for range w.layer.kick {
		w.mu.Lock()
		if seq := w.layer.renderLive(); len(seq) > 0 {
			var b bytes.Buffer
			b.Write(syncBegin)
			wrap(&b, seq)
			b.Write(syncEnd)
			_, _ = w.File.Write(b.Bytes())
		}
		w.mu.Unlock()
	}
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
