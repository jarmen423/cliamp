package ui

// vis_scene_input.go listens to the music for the pixel scenes (Ridgeline,
// EventHorizon, Stage). Next to the usual spectrum it finds drum hits in
// three frequency ranges and the drops, and reads the notes on a
// three-octave keyboard and the bass note from a longer FFT window.

import (
	"math"
	"time"
)

const (
	// SceneBands is the scenes' spectrum resolution.
	SceneBands = 64
	// SceneKeys is the keyboard the note analysis fills: C3 to B5.
	SceneKeys = 36
	// SceneKey0 is the MIDI note of the first key (C3).
	SceneKey0 = 48
	// noteFFTSize is the note analysis window: about 6 Hz per bin, enough to
	// tell neighboring semitones apart from C3 up.
	noteFFTSize = 8192
)

// sceneSpec is the spectrum analysis of the scenes. They read the audible
// tap so the drums land on the beat that is heard, not one buffer early.
var sceneSpec = VisAnalysisSpec{BandCount: SceneBands, FFTSize: defaultFFTSize, Tap: VisTapAudible}

// SceneFrame is one snapshot of what the scenes draw from. Drum hits are
// running counts, not flags, so a renderer that skips snapshots still sees
// every hit.
type SceneFrame struct {
	T                            float64 // seconds on the analysis clock
	Bands                        [SceneBands]float64
	Kicks, Snares, Hats, Crashes uint64
	Keys                         [SceneKeys]float64
	BassNote                     int // MIDI note, -1 when there is none
}

// sceneAnalyzer turns analysis ticks into SceneFrames.
type sceneAnalyzer struct {
	started   bool
	primed    bool // prev holds a real spectrum, so flux means something
	t0        time.Time
	t         float64
	prev      []float64
	kick      bandOnset
	snare     bandOnset
	hat       bandOnset
	lastKick  float64
	lastCrash float64
	frame     SceneFrame
	notes     []float64 // the note window, copied out of the wave buffer

	cbuf   []complex128
	tw     []complex128
	window []float64
}

func newSceneAnalyzer() *sceneAnalyzer {
	// 64 bands follow the legacy edges: the first 7 lie under 100 Hz, bands
	// 15-30 span about 250 Hz to 1.5 kHz, 45-57 about 6 to 15 kHz. Analysis
	// ticks come about 30 times a second, which sets the gaps.
	a := &sceneAnalyzer{
		kick:      bandOnset{from: 0, to: 7.0 / SceneBands, k: 2.0, floor: 0.2, gap: 7},
		snare:     bandOnset{from: 15.0 / SceneBands, to: 31.0 / SceneBands, k: 2.0, floor: 0.3, gap: 4},
		hat:       bandOnset{from: 45.0 / SceneBands, to: 58.0 / SceneBands, k: 1.8, floor: 0.25, gap: 3},
		lastKick:  -9,
		lastCrash: -9,
		prev:      make([]float64, SceneBands),
		frame:     SceneFrame{BassNote: -1},
	}
	a.kick.reset()
	a.snare.reset()
	a.hat.reset()
	return a
}

// update folds one analysis tick into the frame. now is the tick's wall
// clock (zero in tests that do not set it: the clock then advances one
// analysis interval). sr is the sample rate of notes.
func (a *sceneAnalyzer) update(now time.Time, bands, notes []float64, sr float64) {
	switch {
	case !a.started:
		a.started, a.t0 = true, now
	case now.IsZero() || a.t0.IsZero():
		a.t += TickAnalyze.Seconds()
	default:
		a.t = now.Sub(a.t0).Seconds()
	}
	t := a.t
	f := &a.frame
	f.T = t
	if len(bands) == SceneBands && !a.primed {
		copy(a.prev, bands)
		a.primed = true
	} else if len(bands) == SceneBands {
		copy(f.Bands[:], bands)
		kick, _ := a.kick.step(bands, a.prev)
		snare, _ := a.snare.step(bands, a.prev)
		hat, hatStrength := a.hat.step(bands, a.prev)
		copy(a.prev, bands)
		if snare {
			f.Snares++
		}
		if hat {
			f.Hats++
		}
		// A crash: the first kick after a quiet stretch (a drop), or a
		// cymbal far louder than the hats around it.
		drop := kick && t-a.lastKick > 2.5
		if (drop || hatStrength > 4) && t-a.lastCrash > 2 {
			f.Crashes++
			a.lastCrash = t
		}
		if kick {
			f.Kicks++
			a.lastKick = t
		}
	}
	a.listenNotes(notes, sr)
}

// listenNotes fills the keyboard and the bass note from the note window. A
// key is lit relative to the loudest key, so it shows which notes stand out
// of the mix, not their absolute level.
func (a *sceneAnalyzer) listenNotes(samples []float64, sr float64) {
	f := &a.frame
	n := noteFFTSize
	for n > len(samples) && n > 1024 {
		n /= 2
	}
	if len(samples) < n || sr <= 0 {
		for k := range f.Keys {
			f.Keys[k] *= 0.8
		}
		f.BassNote = -1
		return
	}
	if len(a.cbuf) != n {
		a.cbuf = make([]complex128, n)
		a.tw = buildTwiddles(n)
		a.window = buildHannWindow(n)
	}
	src := samples[len(samples)-n:]
	for i, s := range src {
		a.cbuf[i] = complex(s*a.window[i], 0)
	}
	fftInPlace(a.cbuf, a.tw)
	binHz := sr / float64(n)
	power := func(b int) float64 {
		if b <= 0 || b >= n/2 {
			return 0
		}
		c := a.cbuf[b]
		return real(c)*real(c) + imag(c)*imag(c)
	}
	db := func(p float64) float64 { return 10 * math.Log10(p+1e-12) }

	var keyDB [SceneKeys]float64
	top := -300.0
	q := math.Pow(2, 1.0/24) // a quarter tone either side of the key
	for k := range keyDB {
		// Only a spectral peak counts for a key, so the skirt of a loud
		// neighbor does not light it.
		hz := 440 * math.Pow(2, float64(SceneKey0+k-69)/12)
		best := power(int(math.Round(hz/binHz))) * 0.05
		for b := int(hz / q / binHz); b <= int(math.Ceil(hz*q/binHz)); b++ {
			if p := power(b); p >= power(b-1) && p >= power(b+1) {
				best = max(best, p)
			}
		}
		keyDB[k] = db(best)
		top = max(top, keyDB[k])
	}
	for k, d := range keyDB {
		f.Keys[k] = 0
		if d > 10 { // above the noise floor of a quiet mix
			f.Keys[k] = clamp01((d - (top - 18)) / 18)
		}
	}

	bestB, bestP := 0, 0.0
	for b := int(38 / binHz); b <= int(math.Ceil(200/binHz)); b++ {
		if p := power(b); p > bestP {
			bestB, bestP = b, p
		}
	}
	f.BassNote = -1
	if bestB > 0 && db(bestP) > top-12 {
		// Parabolic interpolation over the peak bin and its neighbors.
		l, c, r := db(power(bestB-1)), db(bestP), db(power(bestB+1))
		off := 0.0
		if den := l - 2*c + r; den != 0 {
			off = max(-0.5, min(0.5, 0.5*(l-r)/den))
		}
		hz := (float64(bestB) + off) * binHz
		f.BassNote = int(math.Round(69 + 12*math.Log2(hz/440)))
	}
}

// sceneDriver drives a pixel scene: it reads the note window and the 64-band
// spectrum each analysis tick and draws the half-block text rendition.
type sceneDriver struct{ mode VisMode }

func newSceneDriver(mode VisMode) func() visModeDriver {
	return func() visModeDriver { return &sceneDriver{mode: mode} }
}

func (*sceneDriver) AnalysisSpec(*Visualizer) VisAnalysisSpec { return sceneSpec }

func (d *sceneDriver) Render(v *Visualizer) string { return v.renderPixelText(d.mode) }

func (*sceneDriver) Tick(v *Visualizer, ctx VisTickContext) {
	if ctx.OverlayActive {
		v.lastAnalyzeAt = time.Time{}
		v.lastSmoothTick = time.Time{}
		return
	}
	if v.scene == nil {
		v.scene = newSceneAnalyzer()
	}
	due := v.lastAnalyzeAt.IsZero() || ctx.Now.IsZero() || ctx.Now.Sub(v.lastAnalyzeAt) >= TickAnalyze
	if ctx.Analyze != nil && due {
		// A raw read first: it leaves the note window in waveBuf. The band
		// analysis then reads the newest end of the same samples.
		ctx.Analyze(VisAnalysisSpec{FFTSize: noteFFTSize})
		v.scene.notes = append(v.scene.notes[:0], v.waveBuf...)
		if bands := ctx.Analyze(sceneSpec); bands != nil {
			v.bands = bands
		}
		v.scene.update(ctx.Now, v.bands, v.scene.notes, v.sr)
		if !ctx.Now.IsZero() {
			v.lastAnalyzeAt = ctx.Now
		}
	}
	v.advanceSmoothing(ctx.Now)
}

func (*sceneDriver) TickInterval(_ *Visualizer, ctx VisTickContext) time.Duration {
	if ctx.Playing && !ctx.OverlayActive {
		return TickAnim
	}
	return defaultDriverTickInterval(ctx)
}

func (*sceneDriver) OnEnter(*Visualizer) {}

func (*sceneDriver) OnLeave(*Visualizer) {}
