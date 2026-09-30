package model

import (
	"bytes"
	"image"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/bjarneo/cliamp/ui"
	"github.com/bjarneo/cliamp/ui/termimg"
)

// setImageEnv clears the variables SetImageMode reads, then applies env.
func setImageEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, k := range []string{"TERM", "TERM_PROGRAM", "TMUX"} {
		t.Setenv(k, env[k])
	}
}

func TestArtKindDetection(t *testing.T) {
	ok := uv.KittyGraphicsEvent{Options: kitty.Options{ID: termimg.KittyQueryID}, Payload: []byte("OK")}
	refused := uv.KittyGraphicsEvent{Options: kitty.Options{ID: termimg.KittyQueryID}, Payload: []byte("ENOTSUPPORTED:no")}
	sixel := uv.PrimaryDeviceAttributesEvent{62, 4, 22}
	plain := uv.PrimaryDeviceAttributesEvent{62, 22}
	tests := []struct {
		name string
		mode string
		env  map[string]string
		msgs []tea.Msg
		want artKind
	}{
		{"ghostty answers", "auto", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "ghostty 1.3.1"}, plain}, artKitty},
		{"kitty answers", "auto", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "kitty(0.42.1)"}, plain}, artKitty},
		{"kitty wins over sixel", "auto", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "kitty(0.42.1)"}, sixel}, artKitty},
		{"protocol without placeholders", "auto", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "WezTerm 20240203"}, sixel}, artSixel},
		{"unnamed terminal identified by env", "auto", map[string]string{"TERM": "xterm-ghostty"}, []tea.Msg{ok, plain}, artKitty},
		{"env alone is not enough", "auto", map[string]string{"TERM": "xterm-kitty"}, []tea.Msg{plain}, artBlocks},
		{"ghostty via TERM_PROGRAM", "auto", map[string]string{"TERM": "tmux-256color", "TERM_PROGRAM": "ghostty"}, []tea.Msg{ok, plain}, artKitty},
		{"query refused", "auto", nil, []tea.Msg{refused, tea.TerminalVersionMsg{Name: "kitty"}, sixel}, artSixel},
		{"sixel only", "auto", nil, []tea.Msg{sixel}, artSixel},
		{"nothing", "auto", nil, []tea.Msg{plain}, artBlocks},
		{"before any reply", "auto", nil, nil, artBlocks},
		{"forced kitty", "kitty", nil, []tea.Msg{sixel}, artKitty},
		{"forced sixel", "sixel", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "kitty"}}, artSixel},
		{"forced blocks", "blocks", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "kitty"}}, artBlocks},
		{"off", "off", nil, []tea.Msg{ok, tea.TerminalVersionMsg{Name: "kitty"}}, artNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setImageEnv(t, tt.env)
			m := Model{art: newArtStore()}
			m.SetImageMode(tt.mode)
			for _, msg := range tt.msgs {
				if !m.handleTermImageEvent(msg) {
					t.Fatalf("%T not handled", msg)
				}
			}
			if got := m.artKindNow(); got != tt.want {
				t.Fatalf("kind %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTermImageQueries(t *testing.T) {
	tests := []struct {
		mode  string
		kitty bool
	}{
		{"auto", true},
		{"kitty", true},
		{"sixel", false},
		{"blocks", false},
	}
	for _, tt := range tests {
		m := Model{}
		m.SetImageMode(tt.mode)
		raw, ok := m.termImageQueries()().(tea.RawMsg)
		if !ok {
			t.Fatalf("%s: queries are not one Raw", tt.mode)
		}
		q := raw.Msg.(string)
		if !strings.HasSuffix(q, ansi.RequestPrimaryDeviceAttributes) {
			t.Errorf("%s: DA1 must come last to fence the other replies: %q", tt.mode, q)
		}
		if got := strings.HasPrefix(q, termimg.KittyQuery()+ansi.RequestNameVersion); got != tt.kitty {
			t.Errorf("%s: kitty probe sent=%v, want %v: %q", tt.mode, got, tt.kitty, q)
		}
	}
}

// kittyGridModel is an immersive grid of covers ready to encode, with its
// kitty output captured.
func kittyGridModel(t *testing.T, mode string) (*Model, *bytes.Buffer) {
	t.Helper()
	setImageEnv(t, nil)
	m := immersiveModel(t)
	m.SetImageLayer(termimg.NewLayer())
	out := &bytes.Buffer{}
	m.SetImageOutput(out)
	m.SetImageMode(mode)
	m.immersive.mode = immCanvasGrid
	for i := range m.immersive.lists {
		u := "file:///cover" + string(rune('a'+i)) + ".png"
		m.immersive.lists[i].ImageURL = u
		img := image.NewRGBA(image.Rect(0, 0, 40, 40))
		for p := 0; p < len(img.Pix); p += 4 {
			img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = byte(60*i), 90, 200, 255
		}
		m.art.imgs[u] = &artImage{img: img}
	}
	return m, out
}

// runArt runs the art request loop until every cover is encoded.
func runArt(t *testing.T, m *Model) {
	t.Helper()
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case artFetchedMsg:
			c, _ := m.handleArtMsg(msg)
			run(c)
		case artEncodedMsg:
			c, _ := m.handleArtMsg(msg)
			run(c)
		}
	}
	run(m.immArtRequests())
}

// Kitty covers are text: every art box becomes placeholder cells of the
// box's exact size, and nothing else in the frame moves compared with the
// text placeholders the frame shows without images.
func TestImmersiveKittyFrameKeepsLayout(t *testing.T) {
	m, out := kittyGridModel(t, "kitty")
	runArt(t, m)
	slots := m.immArtSlots()
	if len(slots) != len(m.immersive.lists)+1 {
		t.Fatalf("%d art slots, want one per playlist plus Now Playing", len(slots))
	}
	if n := strings.Count(out.String(), "a=T,U=1"); n != len(slots) {
		t.Fatalf("%d transmissions, want %d", n, len(slots))
	}
	kittyFrame := m.View().Content

	off := *m
	off.imgMode = imagesOff
	offFrame := off.View().Content

	kl, ol := strings.Split(kittyFrame, "\n"), strings.Split(offFrame, "\n")
	if len(kl) != len(ol) {
		t.Fatalf("%d lines with kitty, %d without", len(kl), len(ol))
	}
	for i := range kl {
		if kw, ow := ansi.StringWidth(kl[i]), ansi.StringWidth(ol[i]); kw != ow {
			t.Fatalf("line %d is %d cells with kitty, %d without", i, kw, ow)
		}
	}

	inArt := map[[2]int]bool{}
	ox, oy := m.layout.paddingH, m.layout.paddingV
	for _, s := range slots {
		for y := range s.rect.H {
			for x := range s.rect.W {
				inArt[[2]int{ox + s.rect.X + x, oy + s.rect.Y + y}] = true
			}
		}
	}
	w, h := m.width, len(kl)
	for _, method := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		kb, ob := uv.NewScreenBuffer(w, h), uv.NewScreenBuffer(w, h)
		kb.Method, ob.Method = method, method
		uv.NewStyledString(kittyFrame).Draw(kb, kb.Bounds())
		uv.NewStyledString(offFrame).Draw(ob, ob.Bounds())
		for y := range h {
			for x := range w {
				kc, oc := kb.CellAt(x, y), ob.CellAt(x, y)
				if inArt[[2]int{x, y}] {
					if kc == nil || kc.Width != 1 || !strings.HasPrefix(kc.Content, string(kitty.Placeholder)) {
						t.Fatalf("method %v: art cell %d,%d = %+v, want a placeholder", method, x, y, kc)
					}
					continue
				}
				if (kc == nil) != (oc == nil) || (kc != nil && kc.Content != oc.Content) {
					t.Fatalf("method %v: cell %d,%d = %+v with kitty, %+v without", method, x, y, kc, oc)
				}
			}
		}
	}
}

// The pixel visualizer band is one kitty image's placeholder cells; the
// Sixel layer is never armed for it.
func TestImmersiveKittyPixelVisBand(t *testing.T) {
	m, _ := kittyGridModel(t, "kitty")
	m.vis = ui.NewVisualizer(48000)
	m.vis.Mode = ui.VisAurora
	if kind := m.immPixelVisOn(); kind != artKitty {
		t.Fatalf("pixel vis kind %d, want kitty", kind)
	}
	g := m.immGeom()
	lines := m.renderImmVis(g.w, g.visH)
	want := termimg.KittyPlaceholders(kittyVisID, g.w, g.visH)
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatal("band is not the visualizer image's placeholders")
	}
}

// With kitty images the output layer never engages: frames reach the
// terminal byte for byte.
func TestImmersiveKittyLeavesWriterPassThrough(t *testing.T) {
	m, _ := kittyGridModel(t, "kitty")
	m.vis = ui.NewVisualizer(48000)
	m.vis.Mode = ui.VisAurora
	layer := termimg.NewLayer()
	m.imgLayer = layer
	runArt(t, m)
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := termimg.NewWriter(f, layer)
	frame := "\x1b[?2026h" + m.View().Content + "\x1b[?2026l"
	// A live Sixel frame for the band is dropped unless View armed it.
	g := m.immGeom()
	layer.SetLive(termimg.Placement{Key: "live", X: m.layout.paddingH, Y: m.layout.paddingV, W: g.w, H: g.visH, Data: []byte("<LIVE>")})
	if _, err := w.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(f.Name())
	if string(got) != frame {
		t.Fatalf("writer altered a kitty frame (%d bytes in, %d out)", len(frame), len(got))
	}
}

func kittyDeletes(s string) int { return strings.Count(s, "a=d,d=I") }

func TestKittyImagesDeletedOnExit(t *testing.T) {
	m, out := kittyGridModel(t, "kitty")
	runArt(t, m)
	n := m.art.kittyCountLocked()
	out.Reset()
	m.exitImmersive()
	if got := kittyDeletes(out.String()); got != n+1 {
		t.Fatalf("%d deletes, want %d covers + the visualizer", got, n+1)
	}
	if !m.pixVis.resend.Load() {
		t.Fatal("visualizer not told to resend its deleted image")
	}
	if m.art.kittyCountLocked() != 0 {
		t.Fatal("kitty encodings survive release")
	}
	out.Reset()
	m.releaseImages()
	if got := kittyDeletes(out.String()); got != 1 {
		t.Fatalf("second release sent %d deletes, want only the visualizer's", got)
	}
}

// Past artMaxKitty images, covers that are not on screen are deleted from
// the terminal before a new one is sent; on-screen ones stay.
func TestKittyEvictsOffscreenImages(t *testing.T) {
	m, out := kittyGridModel(t, "kitty")
	cw, ch := m.cellPx()
	for i := range artMaxKitty {
		k := artKey{url: "file:///old" + string(rune('A'+i%26)) + string(rune('a'+i/26)), w: 6, h: 3, cellW: cw, cellH: ch, kind: artKitty}
		m.art.encs[k] = &artEncoding{kittyID: termimg.KittyID(1000 + i), kitty: []string{"x"}}
	}
	m.art.encs[artKey{url: "file:///blocks", w: 6, h: 3, kind: artBlocks}] = &artEncoding{blocks: []string{"x"}}
	runArt(t, m)
	if got := kittyDeletes(out.String()); got != artMaxKitty {
		t.Fatalf("%d deletes, want %d", got, artMaxKitty)
	}
	if n := m.art.kittyCountLocked(); n != len(m.immArtSlots()) {
		t.Fatalf("%d kitty encodings left, want the %d on screen", n, len(m.immArtSlots()))
	}
	if m.art.encs[artKey{url: "file:///blocks", w: 6, h: 3, kind: artBlocks}] == nil {
		t.Fatal("eviction dropped a non-kitty encoding")
	}
}

// An encode that lands after its entry was dropped is discarded rather
// than transmitted, so no image leaks into the terminal.
func TestKittyStaleEncodeIsDropped(t *testing.T) {
	m, out := kittyGridModel(t, "kitty")
	cmd := m.immArtRequests()
	m.releaseImages()
	out.Reset()
	var msgs []tea.Msg
	for _, c := range cmd().(tea.BatchMsg) {
		msgs = append(msgs, c())
	}
	for _, msg := range msgs {
		m.handleArtMsg(msg)
	}
	if out.Len() != 0 {
		t.Fatalf("stale encodes were transmitted: %d bytes", out.Len())
	}
}
