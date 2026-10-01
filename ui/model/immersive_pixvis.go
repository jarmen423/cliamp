package model

// immersive_pixvis.go drives the pixel visualizers (Aurora, Phosphor) as
// images in the immersive visualizer band. View hands the worker a snapshot
// of the spectrum and waveform each frame; the worker renders and encodes
// off the UI goroutine. With Sixel it publishes the result as the layer's
// live placement, which the output pump draws between Bubbletea frames, and
// the band renders as blank cells. With kitty graphics it retransmits one
// image ID each frame and the band renders as that image's placeholder
// cells, which never change.

import (
	"image"
	"image/color"
	"io"
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

// pixVisJob is one frame request, in screen cells and pixels.
type pixVisJob struct {
	mode        ui.VisMode
	colors      ui.PixelColors
	bands, wave []float64
	x, y, w, h  int       // cells
	pw, ph      int       // pixels
	kitty       io.Writer // kitty output; nil for Sixel
	tmux        bool
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
		if img == nil || img.Rect.Dx() != job.pw || img.Rect.Dy() != job.ph {
			img = image.NewRGBA(image.Rect(0, 0, job.pw, job.ph))
		}
		pv.Draw(img, job.bands, job.wave)
		if job.kitty != nil {
			changed := !kenc.Unchanged(img)
			if w.resend.Swap(false) || changed {
				_, _ = job.kitty.Write(kenc.Encode(kittyVisID, img, job.w, job.h, job.tmux))
			}
			continue
		}
		if p := ui.PixelPalette(job.colors); enc == nil || !slices.Equal(p, pal) {
			pal, enc = p, termimg.NewSixelEncoder(p, 0)
		}
		seq++
		w.layer.SetLive(termimg.Placement{
			Key: "vis#" + strconv.Itoa(seq),
			X:   job.x, Y: job.y, W: job.w, H: job.h,
			Data: enc.Encode(img),
		})
	}
}

// immPixelVisOn reports how the band shows a pixel visualizer as an image
// this frame: artSixel, artKitty, or artNone when the text renderer draws it
// (pixel modes included).
func (m Model) immPixelVisOn() artKind {
	if m.pixVis == nil || m.vis == nil || !ui.IsPixelMode(m.vis.Mode) ||
		m.visualizerDisabled() || m.activeScreen() != screenImmersive {
		return artNone
	}
	switch kind := m.artKindNow(); kind {
	case artSixel:
		return kind
	case artKitty:
		if g := m.immGeom(); m.imgOut != nil && g.w <= termimg.KittyMaxCells && g.visH <= termimg.KittyMaxCells {
			return kind
		}
	}
	return artNone
}

// immKittyVisCells is the band's placeholder cells, rebuilt only when the
// band's size changes.
func (m Model) immKittyVisCells(w, rows int) []string {
	v := m.pixVis
	if len(v.kittyCells) != rows || v.kittyW != w {
		v.kittyCells, v.kittyW = termimg.KittyPlaceholders(kittyVisID, w, rows), w
	}
	return v.kittyCells
}

// feedPixelVis requests the next band frame. The Sixel live image is armed
// only while the band shows a Sixel visualizer.
func (m Model) feedPixelVis() {
	kind := m.immPixelVisOn()
	g := m.immGeom()
	if kind == artSixel {
		m.imgLayer.ArmLive(m.layout.paddingH, m.layout.paddingV, g.w, g.visH)
	} else {
		m.imgLayer.ArmLive(0, 0, 0, 0) // kitty frames bypass the layer
	}
	if kind == artNone {
		return
	}
	cw, ch := m.cellPx()
	job := pixVisJob{
		mode: m.vis.Mode, colors: ui.CurrentPixelColors(),
		x: m.layout.paddingH, y: m.layout.paddingV, w: g.w, h: g.visH,
		pw: g.w * cw, ph: g.visH * ch,
		tmux: m.imgTmux,
	}
	if kind == artKitty {
		job.kitty = m.imgOut
	}
	job.bands, job.wave = m.vis.PixelInput()
	m.pixVis.submit(job)
}
