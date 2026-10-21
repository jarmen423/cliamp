package ui

// vis_scene.go holds what the pixel scenes (Ridgeline, EventHorizon, Stage,
// Encore) share: the per-frame context they draw from and the canvas sizing.

import (
	"math"
	"math/rand/v2"
)

// pixelScene is one scene. draw advances its state by s.dt (nothing when
// the snapshot repeats) and paints the whole canvas.
type pixelScene interface {
	reset(w, h int)
	draw(c *canvas, s *sceneCtx)
}

func isSceneMode(mode VisMode) bool {
	return mode == VisRidgeline || mode == VisEventHorizon || mode == VisStage || mode == VisEncore
}

func newScene(mode VisMode) pixelScene {
	switch mode {
	case VisEventHorizon:
		return &horizonScene{}
	case VisStage:
		return &stageScene{}
	case VisEncore:
		return &encoreScene{}
	default:
		return &ridgeScene{}
	}
}

// sceneRand gives each scene the same stars and scatter every run, so the
// frames are reproducible.
func sceneRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// sceneCtx is one frame's input: the latest SceneFrame, the drum hits since
// the previous frame, envelopes that decay with time, a few spectrum
// summaries, and the theme colors.
type sceneCtx struct {
	SceneFrame
	dt float64 // seconds since the previous frame; 0 when the snapshot repeats
	fr float64 // dt in 25 fps frames, the unit the rates below are tuned in

	kick, snare, hat, crash             bool
	kickEnv, snareEnv, hatEnv, crashEnv float64
	beatEnv                             float64

	bass, energy, bassLevel float64

	low, mid, high [3]float32

	started bool
	lastT   float64
	seen    [4]uint64
}

// advance moves the context to snapshot f.
func (s *sceneCtx) advance(f SceneFrame, cs PixelColors) {
	s.low, s.mid, s.high = rgbf(cs.Low), rgbf(cs.Mid), rgbf(cs.High)
	counts := [4]uint64{f.Kicks, f.Snares, f.Hats, f.Crashes}
	switch {
	case !s.started || f.T < s.lastT || counts[0] < s.seen[0]:
		// First frame, or the analysis restarted: take its counts as seen
		// and step one frame so histories have something to show.
		s.started, s.seen, s.dt = true, counts, 0.04
	default:
		s.dt = min(0.1, f.T-s.lastT)
	}
	s.lastT = f.T
	s.SceneFrame = f
	s.fr = s.dt * 25
	s.kick, s.snare, s.hat, s.crash = counts[0] > s.seen[0], counts[1] > s.seen[1], counts[2] > s.seen[2], counts[3] > s.seen[3]
	s.seen = counts
	decay := func(env, k float64, hit bool) float64 {
		if hit {
			return 1
		}
		return env * math.Pow(k, s.fr)
	}
	s.kickEnv = decay(s.kickEnv, 0.72, s.kick)
	s.snareEnv = decay(s.snareEnv, 0.66, s.snare)
	s.hatEnv = decay(s.hatEnv, 0.55, s.hat)
	s.crashEnv = decay(s.crashEnv, 0.95, s.crash)
	s.beatEnv = decay(s.beatEnv, 0.82, s.kick)

	var sum, b, bl float64
	for i, v := range f.Bands {
		sum += v
		if i < 10 {
			b += v
		}
		if i >= 2 && i <= 14 {
			bl += v
		}
	}
	s.bass, s.energy, s.bassLevel = b/10, sum/SceneBands, bl/13
}

// grad runs from the low through the mid to the high theme color.
func (s *sceneCtx) grad(t float64) [3]float32 {
	return gradient3(s.low, s.mid, s.high, float32(clamp01(t)))
}

// Scene canvas bounds: enough rows for Stage's 60-pixel art at its smallest
// scale, and few enough pixels that a full-screen frame fits the worker's
// frame budget.
const (
	sceneMinRows   = 80
	sceneMaxPixels = 650_000
)

// sceneCanvasSize picks the resolution a scene renders at for a w by h
// target: at least sceneMinRows, so the art keeps its shape when it is
// shrunk to half-block text, and at most sceneMaxPixels, so a full-screen
// frame stays cheap (it is scaled up on the way out).
func sceneCanvasSize(w, h int) (int, int) {
	k := 1.0
	if h < sceneMinRows {
		k = math.Ceil(sceneMinRows / float64(h))
	}
	if a := float64(w*h) * k * k; a > sceneMaxPixels {
		k = math.Sqrt(sceneMaxPixels / float64(w*h))
	}
	return max(1, int(float64(w)*k)), max(1, int(float64(h)*k))
}

// wide reports whether a canvas is a strip, like the immersive band, rather
// than a full-screen view. The scenes lay out differently for each.
func wide(w, h int) bool { return w >= 3*h }

// designPx is one pixel of the scenes' design size at this canvas size: the
// line widths and glows were tuned on a 180-row strip and a 640-row screen.
func designPx(w, h int) float64 {
	if wide(w, h) {
		return max(1, float64(h)/180)
	}
	return max(1, float64(h)/640)
}
