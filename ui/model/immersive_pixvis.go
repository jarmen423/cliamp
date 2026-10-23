package model

// immersive_pixvis.go drives the pixel visualizers (Aurora, Phosphor and
// the scenes) as images: in the immersive visualizer band and in the
// full-screen visualizer. View hands the worker a snapshot of the analysis
// each frame; the worker renders and encodes off the UI goroutine. With
// Sixel it publishes the result as the layer's live placement, which the
// output pump draws between Bubbletea frames, and the area renders as blank
// cells. With kitty graphics it retransmits one image ID each frame and the
// area renders as that image's placeholder cells, which never change.

import (
	"image"
	"image/color"
	"io"
	"math"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bjarneo/cliamp/ui"
	"github.com/bjarneo/cliamp/ui/termimg"
)

// pixVisFrame is the pixel visualizer's frame budget (25 fps): enough for
// smooth motion, and it keeps the terminal's Sixel decoding light.
const pixVisFrame = 40 * time.Millisecond

// pixVisKittyMaxPixels caps a kitty frame. The terminal scales the image to
// its cells, so a full-screen frame can be sent smaller than the screen and
// keep the pipe light.
const pixVisKittyMaxPixels = 700_000

// pixVisJob is one frame request, in screen cells and pixels.
type pixVisJob struct {
	mode       ui.VisMode
	colors     ui.PixelColors
	in         ui.PixelFrame
	x, y, w, h int       // cells
	pw, ph     int       // pixels
	kitty      io.Writer // kitty output; nil for Sixel
	tmux       bool
}

// pixVisWorker renders the pixel visualizer frames. Requests arriving while
// it is busy are dropped: the next frame supersedes them.
// Its goroutine starts with the first request.
type pixVisWorker struct {
	jobs  chan pixVisJob
	layer *termimg.Layer
	once  sync.Once
	// resend forces the next kitty frame out even if unchanged (its image
	// was deleted).
	resend atomic.Bool
	// kittyCells caches the band's placeholder lines, kittyW wide (UI
	// goroutine only).
	kittyCells []string
	kittyW     int
}

func newPixVisWorker(layer *termimg.Layer) *pixVisWorker {
	return &pixVisWorker{jobs: make(chan pixVisJob, 1), layer: layer}
}

func (w *pixVisWorker) submit(job pixVisJob) {
	w.once.Do(func() { go w.run() })
	select {
	case w.jobs <- job:
	default:
	}
}

func (w *pixVisWorker) run() {
	var (
		pv   *ui.PixelVis
		enc  *termimg.SixelEncoder
		kenc termimg.KittyEncoder
		pal  []color.RGBA
		img  *image.RGBA
		seq  int
		last time.Time
	)
	for job := range w.jobs {
		if wait := pixVisFrame - time.Since(last); wait > 0 {
			time.Sleep(wait)
			select { // render the newest request that arrived meanwhile
			case newer := <-w.jobs:
				job = newer
			default:
			}
		}
		last = time.Now()
		if pv == nil || pv.Mode() != job.mode {
			pv = ui.NewPixelVis(job.mode)
		}
		pv.SetColors(job.colors)
		if job.kitty != nil {
			if img == nil || img.Rect.Dx() != job.pw || img.Rect.Dy() != job.ph {
				img = image.NewRGBA(image.Rect(0, 0, job.pw, job.ph))
			}
			pv.Draw(img, job.in)
			changed := !kenc.Unchanged(img)
			if w.resend.Swap(false) || changed {
				_, _ = job.kitty.Write(kenc.Encode(kittyVisID, img, job.w, job.h, job.tmux))
			}
			continue
		}
		if p := ui.PixelPalette(job.colors); enc == nil || !slices.Equal(p, pal) {
			pal, enc = p, termimg.NewSixelEncoder(p, 0)
		}
		// Sixel takes the frame at its drawn size: the encoder repeats the
		// palette indices up to the band, never building the large image.
		frame := pv.DrawNative(job.pw, job.ph, job.in)
		seq++
		w.layer.SetLive(termimg.Placement{
			Key: "vis#" + strconv.Itoa(seq),
			X:   job.x, Y: job.y, W: job.w, H: job.h,
			Data: enc.EncodeScaled(frame, job.pw, job.ph),
		})
	}
}

// immPixelVisOn reports how the band shows a pixel visualizer as an image
// this frame: artSixel, artKitty, or artNone when the text renderer draws it
// (pixel modes included).
func (m Model) immPixelVisOn() artKind {
	if m.activeScreen() != screenImmersive {
		return artNone
	}
	g := m.immGeom()
	return m.pixelVisKind(g.w, g.visH)
}

// fullVisPixelOn is immPixelVisOn for the full-screen visualizer, whose
// visualizer is PanelWidth by vis.Rows cells.
func (m Model) fullVisPixelOn() artKind {
	if m.activeScreen() != screenFullVisualizer || m.vis == nil {
		return artNone
	}
	return m.pixelVisKind(ui.PanelWidth, m.vis.Rows)
}

// pixelVisKind reports how a w by h cell area can show the pixel
// visualizer as an image.
func (m Model) pixelVisKind(w, h int) artKind {
	if m.pixVis == nil || m.vis == nil || !ui.IsPixelMode(m.vis.Mode) || m.visualizerDisabled() || w <= 0 || h <= 0 {
		return artNone
	}
	switch kind := m.artKindNow(); kind {
	case artSixel:
		return kind
	case artKitty:
		if m.imgOut != nil && w <= termimg.KittyMaxCells && h <= termimg.KittyMaxCells {
			return kind
		}
	}
	return artNone
}

// pixelVisCells is the text under a kind image of the pixel visualizer in a
// w by rows cell area: the kitty image's placeholder cells, or blank cells
// under the Sixel frames. It reports false for artNone.
func (m Model) pixelVisCells(kind artKind, w, rows int) ([]string, bool) {
	switch kind {
	case artKitty:
		return m.kittyVisCells(w, rows), true
	case artSixel:
		return padPane(nil, w, rows), true
	}
	return nil, false
}

// kittyVisCells is the pixel visualizer's placeholder cells, rebuilt only
// when the area's size changes.
func (m Model) kittyVisCells(w, rows int) []string {
	v := m.pixVis
	if len(v.kittyCells) != rows || v.kittyW != w {
		v.kittyCells, v.kittyW = termimg.KittyPlaceholders(kittyVisID, w, rows), w
	}
	return v.kittyCells
}

// feedPixelVis requests the next frame of the pixel visualizer shown as a
// kind image in the w by h cell area at (x, y) on screen. The Sixel live
// image is armed only while the area shows a Sixel visualizer.
func (m Model) feedPixelVis(kind artKind, x, y, w, h int) {
	if kind == artSixel {
		m.imgLayer.ArmLive(x, y, w, h)
	} else {
		m.imgLayer.ArmLive(0, 0, 0, 0) // kitty frames bypass the layer
	}
	if kind == artNone {
		return
	}
	cw, ch := m.cellPx()
	job := pixVisJob{
		mode: m.vis.Mode, colors: ui.CurrentPixelColors(),
		x: x, y: y, w: w, h: h,
		pw: w * cw, ph: h * ch,
		tmux: m.imgTmux,
	}
	if kind == artKitty {
		job.kitty = m.imgOut
		if a := job.pw * job.ph; a > pixVisKittyMaxPixels {
			k := math.Sqrt(float64(pixVisKittyMaxPixels) / float64(a))
			job.pw, job.ph = max(1, int(float64(job.pw)*k)), max(1, int(float64(job.ph)*k))
		}
	}
	job.in = m.vis.PixelInput()
	m.pixVis.submit(job)
}

// feedImmPixelVis feeds the immersive band.
func (m Model) feedImmPixelVis() {
	g := m.immGeom()
	m.feedPixelVis(m.immPixelVisOn(), m.layout.paddingH, m.layout.paddingV, g.w, g.visH)
}
