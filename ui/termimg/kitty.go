package termimg

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Kitty graphics with Unicode placeholders: an image is transmitted once as
// a virtual placement (U=1) of cols x rows cells, then shown by printing
// placeholder cells (U+10EEEE plus row and column diacritics) whose
// foreground color carries the image ID. The cells are ordinary text, so
// Bubbletea's renderer diffs, scrolls, and erases them like any other text
// and no output interposer is involved.
//
// IDs use the 256-color form of the foreground: the low byte is the color
// index (16-255, so profile downsampling to 16 colors never remaps it) and
// the high byte rides on a third diacritic. That gives KittyIDs IDs.

// KittyIDs is the number of distinct IDs KittyID hands out.
const KittyIDs = 240 * 256

// KittyQueryID is the ID the support query uses. KittyID never returns it.
const KittyQueryID = 1

// KittyMaxCells is the widest or tallest image placeholders can address:
// the number of row/column diacritics the protocol defines.
const KittyMaxCells = 297

// KittyID maps n (taken modulo KittyIDs) to an image ID.
func KittyID(n int) uint32 {
	n %= KittyIDs
	if n < 0 {
		n += KittyIDs
	}
	return uint32(n/240)<<24 | uint32(16+n%240)
}

// KittyQuery asks whether the terminal speaks the kitty graphics protocol:
// a supporting terminal answers with an OK for KittyQueryID, and stores
// nothing.
func KittyQuery() string {
	return ansi.KittyGraphics([]byte("AAAA"), "i="+strconv.Itoa(KittyQueryID), "s=1", "v=1", "a=q", "t=d", "f=24")
}

// KittyEncoder builds kitty graphics transmissions, reusing its buffers
// and compressor across calls; a per-frame animation keeps one.
type KittyEncoder struct {
	row, b64 []byte
	z, out   bytes.Buffer
	zw       *zlib.Writer
	last     []byte // pixels of the previous Encode
}

// KittyTransmit returns the sequences that store img under id as a virtual
// placement of cols x rows cells, replacing any image with that id. The
// terminal scales the image into those cells; sizing img to cols x rows
// cells of pixels keeps it sharp. Pixels are sent as zlib-compressed RGB in
// chunks of at most 4096 base64 bytes. With tmux set, every chunk is wrapped
// for tmux passthrough.
func KittyTransmit(id uint32, img *image.RGBA, cols, rows int, tmux bool) []byte {
	return new(KittyEncoder).Encode(id, img, cols, rows, tmux)
}

// Encode is KittyTransmit. The result is valid until the next call.
func (e *KittyEncoder) Encode(id uint32, img *image.RGBA, cols, rows int, tmux bool) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	e.z.Reset()
	if e.zw == nil {
		e.zw, _ = zlib.NewWriterLevel(&e.z, zlib.BestSpeed)
	} else {
		e.zw.Reset(&e.z)
	}
	for y := range h {
		e.row = e.row[:0]
		off := img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y+y)
		for range w {
			e.row = append(e.row, img.Pix[off], img.Pix[off+1], img.Pix[off+2])
			off += 4
		}
		_, _ = e.zw.Write(e.row)
	}
	_ = e.zw.Close()
	n := base64.StdEncoding.EncodedLen(e.z.Len())
	e.b64 = slices.Grow(e.b64[:0], n)[:n]
	base64.StdEncoding.Encode(e.b64, e.z.Bytes())

	e.out.Reset()
	opts := "a=T,U=1,f=24,o=z,q=2,i=" + strconv.FormatUint(uint64(id), 10) +
		",s=" + strconv.Itoa(w) + ",v=" + strconv.Itoa(h) +
		",c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows)
	payload, first := e.b64, true
	for {
		n := min(kitty.MaxChunkSize, len(payload))
		more := n < len(payload)
		switch {
		case more:
			opts += ",m=1"
		case !first:
			opts += ",m=0"
		}
		writeAPC(&e.out, opts, payload[:n], tmux)
		if !more {
			return e.out.Bytes()
		}
		payload, opts, first = payload[n:], "q=2", false
	}
}

// Unchanged reports whether img has the same pixels as the previous call
// and remembers them, so an animation can skip retransmitting a still frame.
func (e *KittyEncoder) Unchanged(img *image.RGBA) bool {
	if bytes.Equal(e.last, img.Pix) {
		return true
	}
	e.last = append(e.last[:0], img.Pix...)
	return false
}

// KittyDelete returns the sequence that deletes the image with id and frees
// its data.
func KittyDelete(id uint32, tmux bool) []byte {
	seq := ansi.KittyGraphics(nil, "a=d", "d=I", "q=2", "i="+strconv.FormatUint(uint64(id), 10))
	if tmux {
		seq = ansi.TmuxPassthrough(seq)
	}
	return []byte(seq)
}

// KittyPlaceholders returns rows lines of cols placeholder cells showing the
// image with id, each line exactly cols cells wide.
func KittyPlaceholders(id uint32, cols, rows int) []string {
	hi := int(id >> 24)
	sgr := "\x1b[38;5;" + strconv.Itoa(int(id&0xff)) + "m"
	lines := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		b.Grow(len(sgr) + cols*12 + 5)
		b.WriteString(sgr)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
			if hi > 0 {
				b.WriteRune(kitty.Diacritic(hi))
			}
		}
		b.WriteString("\x1b[39m")
		lines[r] = b.String()
	}
	return lines
}

// writeAPC writes one kitty graphics command, wrapped for tmux passthrough
// when tmux is set. The payload is base64, so only the framing has ESCs to
// double.
func writeAPC(b *bytes.Buffer, opts string, payload []byte, tmux bool) {
	esc := "\x1b"
	if tmux {
		b.WriteString("\x1bPtmux;")
		esc = "\x1b\x1b"
	}
	b.WriteString(esc + "_G")
	b.WriteString(opts)
	if len(payload) > 0 {
		b.WriteByte(';')
		b.Write(payload)
	}
	b.WriteString(esc + "\\")
	if tmux {
		b.WriteString("\x1b\\")
	}
}
