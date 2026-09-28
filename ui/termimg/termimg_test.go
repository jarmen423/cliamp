package termimg

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
)

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

func TestLayerRenderDiffs(t *testing.T) {
	a := Placement{Key: "a", X: 1, Y: 2, W: 4, H: 2, Data: []byte("<A>")}
	b := Placement{Key: "b", X: 10, Y: 2, W: 4, H: 2, Data: []byte("<B>")}
	tests := []struct {
		name      string
		want      []Placement
		full      bool
		pre, post string // substrings expected; "" means empty
	}{
		{"first draw", []Placement{a}, false, "", "\x1b[3;2H<A>"},
		{"unchanged draws nothing", []Placement{a}, false, "", ""},
		{"added image draws only it", []Placement{a, b}, false, "", "\x1b[3;11H<B>"},
		{"removed image is erased", []Placement{b}, false, "\x1b[3;2H\x1b[4X\x1b[4;2H\x1b[4X", ""},
		{"full repaint redraws all", []Placement{b}, true, "", "<B>"},
		{"clear erases the rest", nil, false, "\x1b[3;11H\x1b[4X", ""},
	}
	l := NewLayer()
	for _, tt := range tests {
		l.Set(tt.want)
		pre, post := l.render(tt.full)
		if (tt.pre == "") != (len(pre) == 0) || !bytes.Contains(pre, []byte(tt.pre)) {
			t.Fatalf("%s: pre = %q, want %q", tt.name, pre, tt.pre)
		}
		if (tt.post == "") != (len(post) == 0) || !bytes.Contains(post, []byte(tt.post)) {
			t.Fatalf("%s: post = %q, want %q", tt.name, post, tt.post)
		}
	}
}

func TestWriterSplicesAroundFrameText(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l := NewLayer()
	w := NewWriter(f, l)
	old := Placement{Key: "old", X: 0, Y: 0, W: 2, H: 1, Data: []byte("<OLD>")}
	l.Set([]Placement{old})
	frame := "\x1b[?2026hTEXT\x1b[?2026l"
	if _, err := w.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	l.Set([]Placement{{Key: "new", X: 5, Y: 0, W: 2, H: 1, Data: []byte("<NEW>")}})
	if _, err := w.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(f.Name())
	second := string(out[strings.LastIndex(string(out), "\x1b[?2026h"):])
	erase, text, draw := strings.Index(second, "\x1b[2X"), strings.Index(second, "TEXT"), strings.Index(second, "<NEW>")
	if erase < 0 || text < 0 || draw < 0 || !(erase < text && text < draw) {
		t.Fatalf("frame order wrong (erase %d, text %d, draw %d): %q", erase, text, draw, second)
	}
	if !strings.HasSuffix(second, "\x1b[?2026l") {
		t.Fatalf("images must stay inside the synchronized update: %q", second)
	}
}

func TestWriterPassesSmallWritesThrough(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l := NewLayer()
	l.Set([]Placement{{Key: "a", W: 1, H: 1, Data: []byte("<A>")}})
	w := NewWriter(f, l)
	_, _ = w.Write([]byte("\x1b[?25l")) // a mode toggle, not a frame
	out, _ := os.ReadFile(f.Name())
	if string(out) != "\x1b[?25l" {
		t.Fatalf("non-frame write altered: %q", out)
	}
}

func TestFillCoverCropsToAspect(t *testing.T) {
	// Left half red, right half blue, 200x100; a square fill keeps the
	// center, so both colors survive side by side.
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 100 {
				c = color.RGBA{0, 0, 255, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	dst := Fill(src, 10, 10)
	if dst.Rect.Dx() != 10 || dst.Rect.Dy() != 10 {
		t.Fatalf("size = %v", dst.Rect)
	}
	if l, r := dst.RGBAAt(0, 5), dst.RGBAAt(9, 5); l.R < 200 || r.B < 200 {
		t.Fatalf("crop lost a side: left %v right %v", l, r)
	}
}

func TestHalfBlocksLineShape(t *testing.T) {
	lines := HalfBlocks(solid(3, 4, color.RGBA{10, 20, 30, 255}))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for _, l := range lines {
		if strings.Count(l, "▀") != 3 || !strings.Contains(l, "38;2;10;20;30") {
			t.Fatalf("bad line %q", l)
		}
	}
}

func TestEncodeSixelFraming(t *testing.T) {
	out := EncodeSixel(solid(12, 12, color.RGBA{200, 50, 50, 255}))
	s := string(out)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;12;12") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("bad sixel framing: %q", s)
	}
	if strings.Count(s, "-") != 2 { // two six-pixel bands
		t.Fatalf("bands = %d, want 2: %q", strings.Count(s, "-"), s)
	}
}

func TestLayerLiveFramesNeedAFrameFirst(t *testing.T) {
	l := NewLayer()
	l.SetLive(&Placement{Key: "v1", X: 0, Y: 0, W: 10, H: 2, Data: []byte("<V1>")})
	if got := l.renderLive(); got != nil {
		t.Fatalf("live frame drawn before a text frame blanked its cells: %q", got)
	}
	if _, post := l.render(false); !bytes.Contains(post, []byte("<V1>")) {
		t.Fatalf("frame did not draw the live placement: %q", post)
	}
	l.SetLive(&Placement{Key: "v2", X: 0, Y: 0, W: 10, H: 2, Data: []byte("<V2>")})
	if got := l.renderLive(); !bytes.Contains(got, []byte("<V2>")) {
		t.Fatalf("next live frame not drawn between frames: %q", got)
	}
	if pre, _ := l.render(false); len(pre) != 0 {
		t.Fatalf("an animated rect must not be erased between its frames: %q", pre)
	}
	l.SetLive(nil)
	if pre, _ := l.render(false); !bytes.Contains(pre, []byte("\x1b[10X")) {
		t.Fatalf("stopping the animation must erase its rect: %q", pre)
	}
}
