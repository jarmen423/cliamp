package termimg

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"image"
	"image/color"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

func TestKittyID(t *testing.T) {
	tests := []struct {
		n    int
		want uint32
	}{
		{0, 16},
		{239, 255},
		{240, 1<<24 | 16},
		{KittyIDs - 1, 255<<24 | 255},
		{KittyIDs, 16}, // wraps
		{-1, 255<<24 | 255},
	}
	for _, tt := range tests {
		if got := KittyID(tt.n); got != tt.want {
			t.Errorf("KittyID(%d) = %#x, want %#x", tt.n, got, tt.want)
		}
	}
	seen := map[uint32]bool{KittyQueryID: true}
	for n := range KittyIDs {
		id := KittyID(n)
		if lo := id & 0xff; lo < 16 || id&0x00ffff00 != 0 || seen[id] {
			t.Fatalf("KittyID(%d) = %#x: low byte must be 16-255, middle bytes zero, unique", n, id)
		}
		seen[id] = true
	}
}

// kittyChunks splits a transmission into its APC sequences' option and
// payload parts.
func kittyChunks(t *testing.T, seq string) (opts, payloads []string) {
	t.Helper()
	for _, c := range strings.SplitAfter(seq, "\x1b\\") {
		if c == "" {
			continue
		}
		if !strings.HasPrefix(c, "\x1b_G") || !strings.HasSuffix(c, "\x1b\\") {
			t.Fatalf("not an APC G sequence: %q", c)
		}
		o, p, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(c, "\x1b_G"), "\x1b\\"), ";")
		opts, payloads = append(opts, o), append(payloads, p)
	}
	return opts, payloads
}

func TestKittyTransmit(t *testing.T) {
	small := solid(4, 2, color.RGBA{10, 20, 30, 255})
	noisy := image.NewRGBA(image.Rect(0, 0, 200, 120)) // incompressible: several chunks
	r := rand.New(rand.NewPCG(1, 2))
	for i := range noisy.Pix {
		noisy.Pix[i] = byte(r.Uint32())
	}
	tests := []struct {
		name  string
		img   *image.RGBA
		multi bool
	}{
		{"single chunk", small, false},
		{"chunked", noisy, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := string(KittyTransmit(0x02000011, tt.img, 7, 3, false))
			opts, payloads := kittyChunks(t, seq)
			if (len(opts) > 1) != tt.multi {
				t.Fatalf("%d chunks, want multi=%v", len(opts), tt.multi)
			}
			w, h := tt.img.Rect.Dx(), tt.img.Rect.Dy()
			first := "a=T,U=1,f=24,o=z,q=2,i=33554449,s=" + strconv.Itoa(w) + ",v=" + strconv.Itoa(h) + ",c=7,r=3"
			if !strings.HasPrefix(opts[0], first) {
				t.Errorf("first chunk options %q, want prefix %q", opts[0], first)
			}
			var all strings.Builder
			for i, p := range payloads {
				if len(p) > kitty.MaxChunkSize {
					t.Errorf("chunk %d is %d bytes", i, len(p))
				}
				last := i == len(payloads)-1
				switch {
				case len(payloads) == 1 && strings.Contains(opts[i], "m="):
					t.Errorf("single chunk carries m=: %q", opts[i])
				case len(payloads) > 1 && !last && !strings.HasSuffix(opts[i], "m=1"):
					t.Errorf("chunk %d options %q, want m=1", i, opts[i])
				case len(payloads) > 1 && last && opts[i] != "q=2,m=0":
					t.Errorf("last chunk options %q, want q=2,m=0", opts[i])
				}
				all.WriteString(p)
			}
			z, err := base64.StdEncoding.DecodeString(all.String())
			if err != nil {
				t.Fatal(err)
			}
			zr, err := zlib.NewReader(bytes.NewReader(z))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(zr)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != w*h*3 {
				t.Fatalf("decoded %d bytes, want %d RGB bytes", len(raw), w*h*3)
			}
			for i := range w * h {
				if !bytes.Equal(raw[i*3:i*3+3], tt.img.Pix[i*4:i*4+3]) {
					t.Fatalf("pixel %d = %v, want %v", i, raw[i*3:i*3+3], tt.img.Pix[i*4:i*4+3])
				}
			}
		})
	}
}

func TestKittyEncoderReuse(t *testing.T) {
	var e KittyEncoder
	a, b := solid(8, 4, color.RGBA{R: 200, A: 255}), solid(3, 2, color.RGBA{B: 90, A: 255})
	for _, img := range []*image.RGBA{a, b, a} {
		if got, want := string(e.Encode(7, img, 2, 1, true)), string(KittyTransmit(7, img, 2, 1, true)); got != want {
			t.Fatalf("reused encoder output differs from a fresh one")
		}
	}
	if e.Unchanged(a) || !e.Unchanged(a) || e.Unchanged(b) {
		t.Fatal("Unchanged must report only a repeat of the previous pixels")
	}
}

func TestKittyTmuxPassthrough(t *testing.T) {
	plain := string(KittyDelete(42, false))
	if plain != "\x1b_Ga=d,d=I,q=2,i=42\x1b\\" {
		t.Fatalf("delete = %q", plain)
	}
	got := string(KittyDelete(42, true))
	want := "\x1bPtmux;" + strings.ReplaceAll(plain, "\x1b", "\x1b\x1b") + "\x1b\\"
	if got != want {
		t.Fatalf("tmux delete = %q, want %q", got, want)
	}
	seq := string(KittyTransmit(16, solid(2, 2, color.RGBA{A: 255}), 1, 1, true))
	if n := strings.Count(seq, "\x1bPtmux;"); n != 1 || !strings.HasSuffix(seq, "\x1b\x1b\\\x1b\\") {
		t.Fatalf("tmux transmit not wrapped per chunk: %q", seq)
	}
}

func TestKittyQuery(t *testing.T) {
	if got, want := KittyQuery(), "\x1b_Gi=1,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
}

// placeholderCells returns the diacritics of each placeholder cell in l.
func placeholderCells(l string) [][]rune {
	var cells [][]rune
	for _, r := range ansi.Strip(l) {
		if r == kitty.Placeholder {
			cells = append(cells, nil)
		} else if len(cells) > 0 {
			cells[len(cells)-1] = append(cells[len(cells)-1], r)
		}
	}
	return cells
}

func TestKittyPlaceholders(t *testing.T) {
	tests := []struct {
		name       string
		id         uint32
		cols, rows int
		sgr        string
		marks      int // diacritics per cell
	}{
		{"low id", 17, 5, 3, "\x1b[38;5;17m", 2},
		{"high byte id", 3<<24 | 200, 4, 2, "\x1b[38;5;200m", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := KittyPlaceholders(tt.id, tt.cols, tt.rows)
			if len(lines) != tt.rows {
				t.Fatalf("%d lines, want %d", len(lines), tt.rows)
			}
			for r, l := range lines {
				if !strings.HasPrefix(l, tt.sgr) || !strings.HasSuffix(l, "\x1b[39m") {
					t.Fatalf("line %d color wrapping: %q", r, l)
				}
				if w := ansi.StringWidth(l); w != tt.cols {
					t.Fatalf("line %d width %d, want %d", r, w, tt.cols)
				}
				cells := placeholderCells(l)
				if len(cells) != tt.cols {
					t.Fatalf("line %d has %d cells", r, len(cells))
				}
				for c, d := range cells {
					want := []rune{kitty.Diacritic(r), kitty.Diacritic(c)}
					if tt.marks == 3 {
						want = append(want, kitty.Diacritic(int(tt.id>>24)))
					}
					if string(d) != string(want) {
						t.Fatalf("cell %d,%d diacritics %U, want %U", r, c, d, want)
					}
				}
			}
		})
	}
}

// Placeholder cells must survive Bubbletea's renderer as exactly one cell
// each, with every diacritic kept in that cell and the ID color intact,
// under both width methods it may pick.
func TestKittyPlaceholdersRenderAsOneCellEach(t *testing.T) {
	const id, cols, rows = 3<<24 | 77, 6, 2
	lines := KittyPlaceholders(id, cols, rows)
	frame := make([]string, rows)
	for i, l := range lines {
		frame[i] = "ab" + l + "|z"
	}
	for _, method := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		buf := uv.NewScreenBuffer(cols+6, rows)
		buf.Method = method
		uv.NewStyledString(strings.Join(frame, "\n")).Draw(buf, buf.Bounds())
		for r := range rows {
			for c := range cols {
				cell := buf.CellAt(2+c, r)
				want := string([]rune{kitty.Placeholder, kitty.Diacritic(r), kitty.Diacritic(c), kitty.Diacritic(3)})
				if cell == nil || cell.Width != 1 || cell.Content != want {
					t.Fatalf("method %v cell %d,%d = %+v, want one cell %q", method, c, r, cell, want)
				}
				if cell.Style.Fg != ansi.IndexedColor(77) {
					t.Fatalf("method %v cell %d,%d fg %v, want index 77", method, c, r, cell.Style.Fg)
				}
			}
			if got := buf.CellAt(2+cols, r); got == nil || got.Content != "|" {
				t.Fatalf("method %v row %d: text after the image moved: %+v", method, r, got)
			}
		}
	}
}

func BenchmarkKittyTransmitVisBand(b *testing.B) {
	// A 163x9-cell band at 10x21 px cells, drawn like a pixel visualizer:
	// mostly dark with a bright band.
	img := image.NewRGBA(image.Rect(0, 0, 1630, 189))
	for y := 80; y < 110; y++ {
		for x := range 1630 {
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = byte(x), byte(y*2), 200, 255
		}
	}
	b.ReportAllocs()
	var e KittyEncoder
	var n int
	for b.Loop() {
		n = len(e.Encode(KittyID(0), img, 163, 9, false))
	}
	b.ReportMetric(float64(n), "bytes/frame")
}
