package ui

// vis_scene_ridge.go is Ridgeline: recent spectrum frames as mountain ridges
// running back to a horizon, nearer ridges hiding the ones behind, bass in
// the middle, under a striped sun that swells with the bass.

import "math"

// ridgeStep is how often a new ridge joins the front (25 per second).
const ridgeStep = 0.04

type ridgeStar struct{ x, y, ph float64 } // x and y as fractions of the sky

type ridgeScene struct {
	hist  [][SceneBands]float64 // newest first
	acc   float64
	stars []ridgeStar

	// Per-frame buffers, kept to spare the allocations: each ridge's line,
	// the rows the nearer ridges cover, and that cover as each of the glowing
	// front ridges saw it.
	ys     []float64
	cover  []int
	covers [3][]int
}

func (r *ridgeScene) reset(int, int) {
	r.hist = r.hist[:0]
	r.acc = 0
	rng := sceneRand()
	r.stars = make([]ridgeStar, 80)
	for i := range r.stars {
		r.stars[i] = ridgeStar{rng.Float64(), rng.Float64(), rng.Float64() * 2 * math.Pi}
	}
}

func ridgeLines(band bool) int {
	if band {
		return 22
	}
	return 40
}

// step adds the ridges that s.dt is due on a w by h view.
func (r *ridgeScene) step(s *sceneCtx, w, h int) {
	n := ridgeLines(wide(w, h))
	r.acc += s.dt
	for r.acc >= ridgeStep || len(r.hist) == 0 {
		r.hist = append(r.hist, [SceneBands]float64{})
		copy(r.hist[1:], r.hist)
		r.hist[0] = s.Bands
		r.acc = max(0, r.acc-ridgeStep)
	}
	if len(r.hist) > n {
		r.hist = r.hist[:n]
	}
}

func (r *ridgeScene) draw(c *canvas, s *sceneCtx) {
	r.step(s, c.w, c.h)
	r.render(c, s, c.w, c.h)
}

// render paints the scene into the top W by H of c.
func (r *ridgeScene) render(c *canvas, s *sceneCtx, W, H int) {
	band := wide(W, H)
	black := [3]float32{}
	c.rect(0, 0, W, H, black, 1, opOver)
	fw, fh := float64(W), float64(H)
	cx := fw / 2
	hy := math.Round(fh * 0.42)
	if band {
		hy = math.Round(fh * 0.34)
	}
	fy := fh - 1
	px := designPx(W, H)

	for _, st := range r.stars {
		a := 0.15 + 0.2*(math.Sin(s.T*2+st.ph)*0.5+0.5)
		c.plot(int(st.x*fw), int(st.y*hy*0.9), white, float32(a), opOver)
	}

	// The sun: a halo, a gradient disc and the stripes, all above the horizon.
	sr := fh * 0.24
	if band {
		sr = fh * 0.42
	}
	sr *= 1 + s.bass*0.14
	halo := sr * 1.35
	for y := int(hy - halo); y < int(hy); y++ {
		for x := int(cx - halo); x <= int(cx+halo); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-hy
			if d2 := dx*dx + dy*dy; d2 > sr*sr && d2 < halo*halo {
				f := 1 - (math.Sqrt(d2)-sr)/(halo-sr)
				c.plot(x, y, s.high, float32(0.5*f*f), opAdd)
			}
		}
	}
	top := tint(s.high, 0.15)
	for y := int(hy - sr); y < int(hy); y++ {
		dy := hy - (float64(y) + 0.5)
		if dy > sr {
			continue
		}
		t := float32(1 - dy/sr)
		col := mix(top, s.high, t/0.6)
		if t > 0.6 {
			col = mix(s.high, s.mid, (t-0.6)/0.4)
		}
		hw := math.Sqrt(sr*sr - dy*dy)
		c.rect(int(math.Round(cx-hw)), y, int(math.Round(cx+hw)), y+1, col, 1, opOver)
	}
	ns := 7
	if band {
		ns = 5
	}
	for i := range ns {
		f := float64(i) / float64(ns)
		y := int(math.Round(hy - sr*0.62*(1-f)))
		th := max(1, int(math.Round(1+f*sr*0.08)))
		c.rect(int(cx-sr-2), y, int(cx+sr+3), min(y+th, int(hy)), black, 1, opOver)
	}
	c.rect(0, int(hy), W, int(hy)+int(px), s.mid, 0.55, opOver)
	for i := -14; i <= 14; i++ {
		c.line(cx+float64(i)*fw*0.012, hy, cx+float64(i)*fw*0.15, fh, px, s.low, 0.2, opOver)
	}

	// Ridges, front to back. cover[x] is the highest row the nearer ridges
	// already cover in column x: each ridge draws only above it, filling
	// black down from its line to hide what lies behind, so every pixel is
	// filled once instead of once per ridge.
	n := ridgeLines(band)
	const K = 5.0
	inv := 1 / (1 + K)
	ampK := 0.95
	if band {
		ampK = 1.15
	}
	if len(r.cover) != W {
		r.cover = make([]int, W)
		r.ys = make([]float64, ridgeLines(false)*W)
		for i := range r.covers {
			r.covers[i] = make([]int, W)
		}
	}
	cover := r.cover
	for x := range cover {
		cover[x] = H
	}
	type glowPass struct {
		ys    []float64
		cover []int
		lw, r float64
		alpha float32
	}
	var glowBuf [3]glowPass
	glows := glowBuf[:0]
	for k := range r.hist {
		z := float64(k) / float64(n-1)
		p := (1/(1+z*K) - inv) / (1 - inv)
		yb := hy + (fy-hy)*p
		hw := fw * 0.5 * (0.16 + 0.9*p)
		amp := (fy - hy) * ampK * (0.2 + 0.8*p)
		b := &r.hist[k]
		ys := r.ys[k*W : (k+1)*W]
		for x := range W {
			ys[x] = yb
			d := math.Abs(float64(x) + 0.5 - cx)
			if d > hw {
				continue
			}
			a := d / hw * (SceneBands - 1)
			i := int(a)
			v := b[i]
			if i+1 < SceneBands {
				v += (b[i+1] - v) * (a - float64(i))
			}
			taper := min(1, (1-a/(SceneBands-1))*5)
			ys[x] = yb - v*amp*taper
		}
		alpha := float32(0.12 + 0.88*math.Pow(p, 1.4))
		lw := (0.7 + 1.4*p) * px
		if k == 0 {
			lw *= 1 + s.beatEnv*1.2
		}
		prev := ys[0]
		for x := range W {
			c.rect(x, int(ys[x])+1, x+1, min(int(yb)+1, cover[x]), black, 1, opOver)
			y0, y1 := math.Min(prev, ys[x]), math.Max(prev, ys[x])
			prev = ys[x]
			col := ridgeColor(s, (float64(x)-(cx-hw))/(2*hw))
			c.rect(x, int(math.Floor(y0-lw/2)), x+1, min(int(math.Ceil(y1+lw/2)), cover[x]), col, alpha, opOver)
		}
		if k < 3 {
			g := glowPass{ys: ys, cover: r.covers[k], lw: lw, r: 6 * px * (1 + s.beatEnv), alpha: alpha}
			copy(g.cover, cover)
			if !band {
				g.r *= 2
			}
			glows = append(glows, g)
		}
		prev = ys[0]
		for x := range W {
			cover[x] = min(cover[x], int(math.Floor(math.Min(prev, ys[x])-lw/2)))
			prev = ys[x]
		}
	}
	// The glow of the front ridges, behind any ridge nearer than each.
	for i := len(glows) - 1; i >= 0; i-- {
		g := glows[i]
		prev := g.ys[0]
		for x := range W {
			y0, y1 := math.Min(prev, g.ys[x]), math.Max(prev, g.ys[x])
			prev = g.ys[x]
			for d := 1.0; d < g.r; d++ {
				f := float32(0.45 * (1 - d/g.r) * (1 - d/g.r))
				if y := int(y0 - g.lw/2 - d); y < g.cover[x] {
					c.plot(x, y, s.high, f*g.alpha, opAdd)
				}
				if y := int(y1 + g.lw/2 + d); y < g.cover[x] {
					c.plot(x, y, s.high, f*g.alpha*0.5, opAdd)
				}
			}
		}
	}
}

// ridgeColor shades a ridge across its width: the low color at the edges,
// the high color at the center peak.
func ridgeColor(s *sceneCtx, u float64) [3]float32 {
	u = clamp01(u)
	if u > 0.5 {
		u = 1 - u
	}
	if u < 0.36 {
		return mix(s.low, s.mid, float32(u/0.36))
	}
	return mix(s.mid, s.high, float32((u-0.36)/0.14))
}
