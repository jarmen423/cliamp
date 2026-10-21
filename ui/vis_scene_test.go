package ui

import (
	"bytes"
	"image"
	"math"
	"testing"
	"time"
)

// sceneBands returns 64 bands at a quiet floor, with the bands lo..hi raised
// to level.
func sceneBands(lo, hi int, level float64) []float64 {
	b := make([]float64, SceneBands)
	for i := range b {
		b[i] = 0.1
		if i >= lo && i <= hi {
			b[i] = level
		}
	}
	return b
}

func TestSceneAnalyzerFindsDrumHits(t *testing.T) {
	tests := []struct {
		name                string
		lo, hi              int
		kicks, snares, hats uint64
	}{
		{"kick in the lows", 0, 6, 1, 0, 0},
		{"snare in the mids", 15, 30, 0, 1, 0},
		{"hi-hat in the highs", 45, 57, 0, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newSceneAnalyzer()
			now := time.Unix(100, 0)
			for range 10 { // settle the running averages on the floor
				a.update(now, sceneBands(0, -1, 0), nil, 48000)
				now = now.Add(TickAnalyze)
			}
			a.update(now, sceneBands(tt.lo, tt.hi, 0.9), nil, 48000)
			// A second rise right after is inside the minimum gap.
			a.update(now.Add(10*time.Millisecond), sceneBands(tt.lo, tt.hi, 1), nil, 48000)
			f := a.frame
			if f.Kicks != tt.kicks || f.Snares != tt.snares || f.Hats != tt.hats {
				t.Errorf("kicks, snares, hats = %d, %d, %d; want %d, %d, %d",
					f.Kicks, f.Snares, f.Hats, tt.kicks, tt.snares, tt.hats)
			}
		})
	}
}

// playKicks feeds a kick every period for n beats and returns the time of
// the last tick.
func playKicks(a *sceneAnalyzer, start time.Time, period time.Duration, n int) time.Time {
	now := start
	for beat := range n {
		hit := start.Add(time.Duration(beat) * period)
		for ; now.Before(hit); now = now.Add(TickAnalyze) {
			a.update(now, sceneBands(0, -1, 0), nil, 48000)
		}
		a.update(now, sceneBands(0, 6, 0.95), nil, 48000)
		now = now.Add(TickAnalyze)
	}
	return now
}

func TestSceneAnalyzerHearsADrop(t *testing.T) {
	a := newSceneAnalyzer()
	start := time.Unix(100, 0)
	a.update(start.Add(-TickAnalyze), sceneBands(0, -1, 0), nil, 48000) // the first tick only primes the history
	end := playKicks(a, start, 500*time.Millisecond, 12)
	if a.frame.Kicks != 12 {
		t.Fatalf("Kicks = %d, want 12", a.frame.Kicks)
	}
	// The song's first kick counts as a drop; steady kicks after it do not.
	if a.frame.Crashes != 1 {
		t.Fatalf("Crashes = %d after steady kicks, want 1", a.frame.Crashes)
	}
	// Three silent seconds, then a kick: a drop.
	quiet := end.Add(3 * time.Second)
	for now := end; now.Before(quiet); now = now.Add(TickAnalyze) {
		a.update(now, sceneBands(0, -1, 0), nil, 48000)
	}
	a.update(quiet, sceneBands(0, 6, 0.95), nil, 48000)
	if a.frame.Crashes != 2 {
		t.Errorf("Crashes = %d after the drop, want 2", a.frame.Crashes)
	}
}

func TestSceneAnalyzerHearsNotes(t *testing.T) {
	const sr = 48000.0
	tests := []struct {
		name     string
		hz       []float64
		wantKeys []int // MIDI notes that must be lit
		bass     int   // MIDI bass note, or -1
	}{
		{"A4 alone", []float64{440}, []int{69}, -1},
		{"C major triad", []float64{261.63, 329.63, 392.0}, []int{60, 64, 67}, -1},
		{"E2 bass under A4", []float64{82.41, 440}, []int{69}, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples := make([]float64, noteFFTSize)
			for i := range samples {
				for _, hz := range tt.hz {
					samples[i] += 0.2 * math.Sin(2*math.Pi*hz*float64(i)/sr)
				}
			}
			a := newSceneAnalyzer()
			a.update(time.Unix(1, 0), sceneBands(0, -1, 0), samples, sr)
			for _, note := range tt.wantKeys {
				if v := a.frame.Keys[note-SceneKey0]; v < 0.55 {
					t.Errorf("key %d = %.2f, want it lit", note, v)
				}
			}
			lit := 0
			for _, v := range a.frame.Keys {
				if v > 0.55 {
					lit++
				}
			}
			if lit > len(tt.wantKeys)+1 {
				t.Errorf("%d keys lit, want about %d", lit, len(tt.wantKeys))
			}
			if tt.bass >= 0 && a.frame.BassNote != tt.bass {
				t.Errorf("BassNote = %d, want %d", a.frame.BassNote, tt.bass)
			}
		})
	}
}

func TestSceneAnalyzerReleasesKeysInSilence(t *testing.T) {
	a := newSceneAnalyzer()
	a.frame.Keys[5] = 1
	a.frame.BassNote = 40
	a.update(time.Unix(1, 0), sceneBands(0, -1, 0), nil, 48000)
	if a.frame.Keys[5] >= 1 || a.frame.BassNote != -1 {
		t.Errorf("key = %.2f, bass = %d; want the key fading and no bass", a.frame.Keys[5], a.frame.BassNote)
	}
}

// busyFrame is a SceneFrame with music in it at time t.
func busyFrame(t float64, kicks uint64) SceneFrame {
	f := SceneFrame{T: t, Kicks: kicks, Snares: kicks, Hats: 2 * kicks, BassNote: 45}
	for i := range f.Bands {
		f.Bands[i] = 0.3 + 0.5*math.Abs(math.Sin(float64(i)*0.3+t))
	}
	f.Keys[9], f.Keys[16], f.Keys[21] = 0.8, 0.9, 1
	return f
}

func TestScenesDraw(t *testing.T) {
	colors := PixelColors{Low: SpectrumLow, Mid: SpectrumMid, High: SpectrumHigh}
	sizes := []struct{ w, h int }{
		{600, 90},  // the immersive band
		{480, 320}, // full screen
		{40, 8},    // half-block text
	}
	for _, mode := range []VisMode{VisRidgeline, VisEventHorizon, VisStage, VisEncore} {
		for _, sz := range sizes {
			t.Run(visModes[mode].name, func(t *testing.T) {
				render := func() (*image.RGBA, *PixelVis) {
					pv := NewPixelVis(mode)
					pv.SetColors(colors)
					img := image.NewRGBA(image.Rect(0, 0, sz.w, sz.h))
					for i := range 30 {
						pv.Draw(img, PixelFrame{Scene: busyFrame(float64(i)*0.04, uint64(i/8))})
					}
					return img, pv
				}
				img, pv := render()
				lit := 0
				for i := 0; i < len(img.Pix); i += 4 {
					if img.Pix[i]|img.Pix[i+1]|img.Pix[i+2] != 0 {
						lit++
					}
				}
				if lit < sz.w*sz.h/50 {
					t.Fatalf("%dx%d: only %d lit pixels", sz.w, sz.h, lit)
				}
				again, _ := render()
				if !bytes.Equal(img.Pix, again.Pix) {
					t.Fatalf("%dx%d: two runs drew different frames", sz.w, sz.h)
				}
				// The same snapshot again moves nothing.
				before := bytes.Clone(img.Pix)
				pv.Draw(img, PixelFrame{Scene: busyFrame(29*0.04, 29/8)})
				if !bytes.Equal(img.Pix, before) {
					t.Fatalf("%dx%d: a repeated snapshot changed the frame", sz.w, sz.h)
				}
			})
		}
	}
}

func TestSceneCanvasSize(t *testing.T) {
	tests := []struct {
		name         string
		w, h         int
		minH, maxPix int
	}{
		{"half-block text renders taller", 120, 18, sceneMinRows, 0},
		{"the band renders as is", 1200, 180, 180, 1200 * 180},
		{"full screen is capped", 2400, 1000, 0, sceneMaxPixels},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := sceneCanvasSize(tt.w, tt.h)
			if h < tt.minH {
				t.Errorf("height %d, want at least %d", h, tt.minH)
			}
			if tt.maxPix > 0 && w*h > tt.maxPix {
				t.Errorf("%dx%d is %d pixels, want at most %d", w, h, w*h, tt.maxPix)
			}
			if r, want := float64(w)/float64(h), float64(tt.w)/float64(tt.h); math.Abs(r-want)/want > 0.02 {
				t.Errorf("aspect %.2f, want %.2f", r, want)
			}
		})
	}
}

func TestSceneDriverReadsTheNoteWindow(t *testing.T) {
	v := NewVisualizer(48000)
	v.Mode = VisStage
	v.Cols, v.Rows = 40, 5
	var sizes []int
	samples := make([]float64, noteFFTSize)
	for i := range samples {
		samples[i] = 0.3 * math.Sin(2*math.Pi*440*float64(i)/48000)
	}
	v.Tick(VisTickContext{
		Now: time.Unix(1, 0), Playing: true,
		Analyze: func(spec VisAnalysisSpec) []float64 {
			spec = NormalizeAnalysisSpec(spec)
			sizes = append(sizes, spec.FFTSize)
			return v.Analyze(samples[len(samples)-spec.FFTSize:], spec)
		},
	})
	if len(sizes) != 2 || sizes[0] != noteFFTSize || sizes[1] != defaultFFTSize {
		t.Fatalf("Analyze window sizes = %v, want [%d %d]", sizes, noteFFTSize, defaultFFTSize)
	}
	if f := v.PixelInput().Scene; f.Keys[69-SceneKey0] < 0.55 {
		t.Errorf("A4 key = %.2f after a 440 Hz tick, want it lit", f.Keys[69-SceneKey0])
	}
}

// BenchmarkSceneDraw measures one frame of each scene at the immersive band
// size and at a full-screen size. The pixel worker has 40 ms per frame.
func BenchmarkSceneDraw(b *testing.B) {
	colors := PixelColors{Low: SpectrumLow, Mid: SpectrumMid, High: SpectrumHigh}
	for _, mode := range []VisMode{VisRidgeline, VisEventHorizon, VisStage, VisEncore} {
		for _, sz := range []struct {
			name string
			w, h int
		}{{"band", 1200, 180}, {"full", 1200, 800}, {"text", 120, 14}} {
			b.Run(visModes[mode].name+"/"+sz.name, func(b *testing.B) {
				pv := NewPixelVis(mode)
				pv.SetColors(colors)
				img := image.NewRGBA(image.Rect(0, 0, sz.w, sz.h))
				i := 0
				for b.Loop() {
					pv.Draw(img, PixelFrame{Scene: busyFrame(float64(i)*0.04, uint64(i/12))})
					i++
				}
			})
		}
	}
}
