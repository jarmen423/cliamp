package ui

// vis_scene_horizon.go is EventHorizon: a spiral galaxy, seen at a tilt,
// turning around a black hole. A star's distance from the center sets the
// band it shows, bass at the core and treble at the rim. The bass feeds the
// accretion disk, each kick sends a shockwave through the arms and lights
// the photon ring, and a drop fires the jets.

import "math"

type galaxyStar struct {
	r, th, tw float64
	spin      float64 // angular speed: inner stars orbit faster
	band      int
	size      int
}

type horizonShock struct{ r, a float64 }

type horizonScene struct {
	trail  canvas // star persistence
	stars  []galaxyStar
	bg     [][3]float64 // static background stars: x, y, brightness
	shocks []horizonShock
	jet    float64
	spin   float64
	rot    float64
}

// horizonGeom sizes the galaxy for a w by h canvas: the radii of the disk
// in x and y (the tilt), and the shadow radius of the black hole.
func horizonGeom(w, h int) (rx, ry, rs float64) {
	fw, fh := float64(w), float64(h)
	rx = fw * 0.46
	if wide(w, h) {
		rx = fw * 0.48
	}
	ry = math.Min(fh*0.42, rx*0.45)
	rs = math.Min(fh*0.16, fw*0.05)
	return rx, ry, rs
}

func (g *horizonScene) reset(w, h int) {
	g.trail.resize(w, h)
	g.trail.clear()
	rng := sceneRand()
	n := 3400
	if wide(w, h) {
		n = 2200
	}
	g.stars = make([]galaxyStar, n)
	for i := range g.stars {
		r := 0.1 + math.Pow(rng.Float64(), 0.75)*0.9
		scatter := (rng.Float64() + rng.Float64() + rng.Float64() - 1.5) * 0.45 * (1.1 - r*0.5)
		th := float64(i%2)*math.Pi + r*5.2 + scatter // two arms
		if rng.Float64() < 0.18 {
			th = rng.Float64() * 2 * math.Pi // the halo
		}
		size := 1
		if rng.Float64() < 0.08 {
			size = 2
		}
		g.stars[i] = galaxyStar{r: r, th: th, tw: rng.Float64() * 2 * math.Pi, spin: 0.55 / math.Sqrt(r+0.05), band: min(SceneBands-1, int(r*SceneBands)), size: size}
	}
	g.bg = make([][3]float64, 140)
	for i := range g.bg {
		g.bg[i] = [3]float64{rng.Float64() * float64(w), rng.Float64() * float64(h), rng.Float64()}
	}
	g.shocks = g.shocks[:0]
}

func (g *horizonScene) draw(c *canvas, s *sceneCtx) {
	rx, ry, rs := horizonGeom(c.w, c.h)
	cx, cy := float64(c.w)/2, float64(c.h)/2
	k := ry / rx
	px := designPx(c.w, c.h)
	if s.dt > 0 {
		g.step(s, cx, cy, rx, ry, rs)
	}
	copy(c.px, g.trail.px)

	for _, b := range g.bg {
		c.plot(int(b[0]), int(b[1]), white, float32(0.12+0.2*b[2]), opAdd)
	}
	for _, sh := range g.shocks {
		a := float32(sh.a * 0.45)
		c.arc(cx, cy, sh.r, sh.r*k, 0, 2*math.Pi, 1.5*px, s.low, func(float64) float32 { return a }, opAdd)
	}

	// The far half of the disk, the far side bent over and under the hole by
	// its gravity, the shadow, then the near half in front of it.
	g.disk(c, s, cx, cy, rs, k, true)
	hb := float32(0.3 + s.bass*0.7)
	c.arc(cx, cy, rs*1.45, rs*1.3, math.Pi, 2*math.Pi, rs*0.28*(0.6+s.bass*0.6), tint(s.mid, 0.35),
		func(float64) float32 { return 0.55 * hb }, opAdd)
	c.arc(cx, cy, rs*1.3, rs*1.15, 0, math.Pi, rs*0.12, tint(s.mid, 0.2),
		func(float64) float32 { return 0.3 * hb }, opAdd)
	c.disc(cx, cy, rs, [3]float32{}, 1, opOver)
	g.disk(c, s, cx, cy, rs, k, false)
	ring := float32(0.55 + 0.45*s.kickEnv)
	c.arc(cx, cy, rs*1.04, rs*1.04, 0, 2*math.Pi, (1+s.kickEnv*2.5)*px, tint(s.high, 0.6),
		func(float64) float32 { return ring }, opAdd)

	if g.jet > 0.02 {
		for _, d := range []float64{-1, 1} {
			y0 := cy + d*rs*0.9
			y1 := 0.0
			if d > 0 {
				y1 = float64(c.h)
			}
			for y := int(math.Min(y0, y1)); y < int(math.Max(y0, y1)); y++ {
				t := (float64(y) + 0.5 - y0) / (y1 - y0)
				hw := rs * (0.1 + 0.35*t)
				col := mix(tint(s.low, 0.6), s.low, float32(t))
				c.rect(int(cx-hw), y, int(cx+hw)+1, y+1, col, float32(0.85*g.jet*(1-t)), opAdd)
			}
		}
	}
}

// step turns the galaxy and lays this frame's stars into the trail buffer.
func (g *horizonScene) step(s *sceneCtx, cx, cy, rx, ry, rs float64) {
	g.rot += (0.004 + s.energy*0.012 + s.beatEnv*0.006) * s.fr
	g.spin += (0.05 + s.bass*0.12) * s.fr
	g.trail.fade(float32(math.Pow(0.68, s.fr)))
	var bins [16][3]float32
	for i := range bins {
		r := float64(i) / 15
		bins[i] = mix(s.grad(1-r), white, float32(max(0, 0.45-r*0.6)))
	}
	for _, st := range g.stars {
		ang := st.th + g.rot*st.spin
		x, y := cx+math.Cos(ang)*st.r*rx, cy+math.Sin(ang)*st.r*ry
		if dx, dy := x-cx, y-cy; dx*dx+dy*dy < rs*rs*1.21 {
			continue
		}
		v := s.Bands[st.band] * (0.85 + 0.15*math.Sin(s.T*3+st.tw))
		a := float32((0.1 + 0.9*v) * s.fr)
		col := bins[min(15, int(st.r*16))]
		g.trail.rect(int(x), int(y), int(x)+st.size, int(y)+st.size, col, a, opAdd)
	}
	if s.kick {
		g.shocks = append(g.shocks, horizonShock{r: rs * 1.8, a: 1})
	}
	live := g.shocks[:0]
	for _, sh := range g.shocks {
		sh.r += rx * 0.035 * s.fr
		sh.a *= math.Pow(0.9, s.fr)
		if sh.a > 0.05 && sh.r < rx*1.1 {
			live = append(live, sh)
		}
	}
	g.shocks = live
	if s.crash {
		g.jet = 1
	} else {
		g.jet *= math.Pow(0.94, s.fr)
	}
}

// disk draws one half of the accretion disk, the far half (above the
// center) or the near one: white-hot inside, brighter on the side turning
// toward the viewer, with hot spots that orbit faster near the hole. It
// fills pixel by pixel, the disk being a wide band.
func (g *horizonScene) disk(c *canvas, s *sceneCtx, cx, cy, rs, k float64, far bool) {
	const rings = 12
	b := 0.4 + s.bass*0.9
	w := math.Max(1, rs*1.9/rings*1.3)
	r0, r1 := rs*1.55-w/2, rs*3.45+w/2
	y0, y1 := int(math.Floor(cy-r1*k)), int(math.Ceil(cy))
	if !far {
		y0, y1 = int(math.Ceil(cy)), int(math.Ceil(cy+r1*k))
	}
	for y := max(0, y0); y < min(c.h, y1); y++ {
		dy := (float64(y) + 0.5 - cy) / k // back in the disk's own plane
		for x := max(0, int(cx-r1)); x <= min(c.w-1, int(cx+r1)); x++ {
			dx := float64(x) + 0.5 - cx
			rho2 := dx*dx + dy*dy
			if rho2 < r0*r0 || rho2 > r1*r1 {
				continue
			}
			rho := math.Sqrt(rho2)
			th := math.Atan2(dy, dx)
			ct := clamp01((rho/rs - 1.55) / 1.9)
			col := mix(white, s.mid, float32(ct/0.3))
			if ct >= 0.3 {
				col = mix(s.mid, s.high, float32((ct-0.3)/0.7))
			}
			dop := 0.5 - 0.5*dx/rho // cos(th - π)
			turb := 0.72 + 0.28*math.Sin(th*5-g.spin*(3-ct*1.5)+ct*(rings-1)*1.3)
			grain := 0.85 + 0.15*math.Cos(ct*(rings-1)*2*math.Pi)
			edge := min(1, (rho-r0)/w, (r1-rho)/w)
			a := b * (0.2 + 0.8*dop) * turb * (1 - ct*0.55) * grain * 1.6 * edge
			c.plot(x, y, col, float32(a), opAdd)
		}
	}
}
