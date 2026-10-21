package ui

// vis_scene_encore.go is Encore: an arena seen from the stage, its floor and
// its stands full of people holding up phone lights. A light's place across
// the arena sets the band it shows, bass in the middle and treble at the
// sides. The crowd sways with the low end, each kick sends a wave of raised
// arms from the stage to the back wall, snares fire camera flashes, and a
// drop starts a Mexican wave round the bowl.

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
)

// Encore's arena, in meters. The camera stands at the stage edge. The floor
// runs from the stage to encoreBack and is encoreFloorW wide; the stands rise
// from its three far sides.
const (
	encoreBack   = 42.0
	encoreFloorW = 15.0
	encoreRows   = 46   // stand rows
	encoreRowD   = 0.9  // stand row depth
	encoreRise   = 0.62 // stand rise per meter of depth
	encoreSeat   = 0.55 // seat spacing
	encorePhone  = 2.05 // a raised phone over the floor or the seat
	encoreBodyR  = 0.8  // the smallest head radius, in pixels, drawn as a person
)

const (
	fanNone    uint8 = iota // no light: drawn only as a silhouette up front
	fanPhone                // a phone flashlight
	fanLighter              // a lighter, warm and flickering
)

type crowdFan struct {
	u, z, e float64 // lateral, depth and floor height of the person
	reach   float64 // distance from the stage, which the kick waves travel
	xn      float64 // base screen x over the width, for the band and the wave
	band    int
	ph, tw  float64 // sway phase and twinkle rate
	side    float64 // the hand holding the light: -1 left, 1 right
	kind    uint8
}

type encoreFlash struct {
	i int
	a float64
}

type encoreScene struct {
	fans    []crowdFan
	rng     *rand.Rand
	f, yH   float64 // focal length and horizon row, in canvas pixels
	eye     float64 // camera height over the floor
	sway    float64
	ripples [][2]float64 // kick waves: distance from the stage, strength
	waves   []float64    // Mexican waves: screen position over the width
	flashes []encoreFlash
	t       float64
}

// encoreView sets the camera for a w by h canvas: the focal length, the
// horizon row and the eye height. The strip looks out from the stage floor
// over the heads; the screen looks down from a riser, so the near crowd
// fills its lower half.
func encoreView(w, h int) (f, yH, eye float64) {
	if wide(w, h) {
		return 0.3 * float64(w), 0.36 * float64(h), 3.2
	}
	return 0.42 * float64(w), 0.24 * float64(h), 5.5
}

func (g *encoreScene) reset(w, h int) {
	g.f, g.yH, g.eye = encoreView(w, h)
	g.rng = sceneRand()
	rng := g.rng
	fw, fh := float64(w), float64(h)
	g.fans = g.fans[:0]
	add := func(u, z, e, reach float64) {
		x, y := g.project(u, z, e+encorePhone, fw)
		if x < -8 || x > fw+8 || y < -8 || z < 0.5 {
			return
		}
		kind := fanNone
		switch r := rng.Float64(); {
		case r < 0.05:
			kind = fanLighter
		case r < 0.74:
			kind = fanPhone
		}
		if kind == fanNone && 0.12*g.f/z < encoreBodyR {
			return // too far back to show without a light
		}
		if _, yh := g.project(u, z, e+1.6, fw); kind == fanNone && yh > fh+8 {
			return
		}
		xn := x / fw
		side := 1.0
		if rng.Float64() < 0.35 {
			side = -1
		}
		g.fans = append(g.fans, crowdFan{
			u: u, z: z, e: e, reach: reach, xn: xn,
			band: min(SceneBands-1, int(math.Abs(xn-0.5)*2*0.9*SceneBands)),
			ph:   rng.Float64()*1.4 + u*0.09, tw: 1.5 + rng.Float64()*4,
			side: side, kind: kind,
		})
	}
	// The floor: a loose grid, every person a little off it.
	for z := 1.0; z < encoreBack; z += 0.62 {
		for u := -encoreFloorW; u <= encoreFloorW; u += 0.6 {
			add(u+(rng.Float64()-0.5)*0.4, z+(rng.Float64()-0.5)*0.4, 0, z)
		}
	}
	// The stands: rows round the floor's left, back and right sides, with a
	// concourse between the lower and the upper tier and an aisle every
	// dozen meters.
	for r := range encoreRows {
		if r >= 17 && r < 20 {
			continue
		}
		d := float64(r)*encoreRowD + 1.2
		e := d*encoreRise + 0.8
		if r >= 20 {
			e += 2.5 // the upper tier overhangs
		}
		side, back := encoreFloorW+d, encoreBack+d
		run := 2*(back-4) + 2*side
		for p := 0.0; p < run; p += encoreSeat {
			if math.Mod(p+d*0.3, 12) < 1.1 {
				continue
			}
			var u, z float64
			switch {
			case p < back-4: // left side, toward the back
				u, z = -side, 4+p
			case p < back-4+2*side: // the back wall, left to right
				u, z = -side+(p-(back-4)), back
			default: // right side, back toward the stage
				u, z = side, back-(p-(back-4+2*side))
			}
			add(u, z, e, z+d)
		}
	}
	// Back to front, so the people up front cover the lights behind them.
	slices.SortStableFunc(g.fans, func(a, b crowdFan) int { return cmp.Compare(b.z, a.z) })
	g.ripples, g.waves, g.flashes = g.ripples[:0], g.waves[:0], g.flashes[:0]
}

// project returns the screen position of the point u across, z deep and e
// over the floor.
func (g *encoreScene) project(u, z, e, fw float64) (x, y float64) {
	k := g.f / z
	return fw/2 + u*k, g.yH + (g.eye-e)*k
}

func (g *encoreScene) step(s *sceneCtx) {
	g.t += s.dt
	g.sway += (0.045 + 0.07*s.energy) * s.fr
	if s.kick {
		g.ripples = append(g.ripples, [2]float64{0, 0.6 + 0.4*s.bass})
	}
	live := g.ripples[:0]
	for _, r := range g.ripples {
		r[0] += 38 * s.dt
		r[1] *= math.Pow(0.97, s.fr)
		if r[0] < encoreBack+60 && r[1] > 0.03 {
			live = append(live, r)
		}
	}
	g.ripples = live
	if s.crash {
		g.waves = append(g.waves, -0.15)
	}
	waves := g.waves[:0]
	for _, x := range g.waves {
		if x += 0.5 * s.dt; x < 1.15 {
			waves = append(waves, x)
		}
	}
	g.waves = waves
	flashes := g.flashes[:0]
	for _, fl := range g.flashes {
		if fl.a *= math.Pow(0.6, s.fr); fl.a > 0.04 {
			flashes = append(flashes, fl)
		}
	}
	g.flashes = flashes
	if s.snare && len(g.fans) > 0 {
		for range 2 + g.rng.IntN(3) {
			i := g.rng.IntN(len(g.fans))
			if g.fans[i].kind != fanNone {
				g.flashes = append(g.flashes, encoreFlash{i: i, a: 1})
			}
		}
	}
}

func (g *encoreScene) draw(c *canvas, s *sceneCtx) {
	if s.dt > 0 {
		g.step(s)
	}
	fw, fh := float64(c.w), float64(c.h)
	px := designPx(c.w, c.h)

	// The dark arena and a haze over the crowd, lit by its lights.
	hazeCol := tint(shade(s.low, 0.5), 0.5)
	for y := range c.h {
		haze := math.Exp(-math.Pow((float64(y)-g.yH)/(fh*0.2), 2)) * (0.03 + 0.05*s.energy)
		c.rect(0, y, c.w, y+1, mix([3]float32{0.004, 0.004, 0.012}, hazeCol, float32(haze)), 1, opOver)
	}
	// The people are dark against the lights, with the haze catching the
	// tops of their heads and the stage washing them on the kick.
	wash := shade(tint(s.grad(0.2), 0.3), float32(0.02+0.07*s.beatEnv))
	rim := shade(tint(s.mid, 0.6), float32(0.07+0.06*s.energy))

	amp := 0.1 + 0.32*s.bassLevel
	twinkle := 0.2 + 0.4*s.hatEnv
	for i := range g.fans {
		p := &g.fans[i]
		k := g.f / p.z
		boost := 0.0
		for _, r := range g.ripples {
			if d := (p.reach - r[0]) / 5; d*d < 9 {
				boost += r[1] * math.Exp(-d*d)
			}
		}
		wave := 0.0
		for _, x := range g.waves {
			if d := (p.xn - x) / 0.05; d*d < 9 {
				wave += math.Exp(-d * d)
			}
		}
		sw := amp * math.Sin(g.sway+p.ph)
		lift := 0.3*boost + 0.7*wave
		lx, ly := g.project(p.u+p.side*0.18+sw, p.z, p.e+encorePhone+lift, fw)
		if hr := 0.12 * k; hr >= encoreBodyR {
			g.person(c, p, k, sw*0.3, lx, ly, fw, shade(wash, float32(8/(8+p.z))), rim)
		}
		if p.kind == fanNone {
			continue
		}
		v := s.Bands[p.band]
		a := (0.35 + 0.9*v) * (1 - twinkle*(0.5+0.5*math.Sin(s.T*p.tw+p.ph*4))) * (1 + 1.6*boost + 1.3*wave)
		col := mix([3]float32{0.85, 0.92, 1}, s.grad(float64(p.band)/SceneBands), 0.4)
		if p.kind == fanLighter {
			col = [3]float32{1, 0.62, 0.25}
			a *= 0.8 + 0.2*math.Sin(s.T*17+p.tw*9)
		}
		encoreLight(c, lx, ly, math.Max(0.45, 0.05*k), col, float32(a))
	}

	for _, fl := range g.flashes {
		p := &g.fans[fl.i]
		x, y := g.project(p.u+p.side*0.18, p.z, p.e+encorePhone, fw)
		r := math.Max(1.2, 0.08*g.f/p.z) * (1 + 2*fl.a)
		encoreLight(c, x, y, r, white, float32(fl.a*2))
		c.line(x-r*3, y, x+r*3, y, px, white, float32(fl.a*0.6), opAdd)
		c.line(x, y-r*3, x, y+r*3, px, white, float32(fl.a*0.6), opAdd)
	}
	g.beams(c, s)
}

// person draws one of the people as a silhouette: the shoulders, the head
// with a rim of light on top and, when they hold a light, the arm raised to
// it.
func (g *encoreScene) person(c *canvas, p *crowdFan, k, sw, lx, ly, fw float64, lit, rim [3]float32) {
	body := [3]float32{0.008 + lit[0], 0.008 + lit[1], 0.016 + lit[2]}
	hx, hy := g.project(p.u+sw, p.z, p.e+1.62, fw)
	_, sy := g.project(p.u, p.z, p.e+1.42, fw)
	_, by := g.project(p.u, p.z, p.e+0.6, fw) // the people in front hide the rest
	if hr := 0.115 * k; hr < 2.5 {
		// Too small for a shape: a body, and a head with a lit top row.
		c.rect(int(hx-0.2*k), int(sy), int(hx+0.2*k)+1, int(by), body, 1, opOver)
		c.rect(int(hx-hr), int(hy-hr), int(hx+hr)+1, int(hy+hr)+1, body, 1, opOver)
		c.rect(int(hx-hr), int(hy-hr), int(hx+hr)+1, int(hy-hr)+1, rim, 1, opOver)
		return
	}
	c.disc(hx-0.13*k, sy+0.05*k, 0.1*k, body, 1, opOver)
	c.disc(hx+0.13*k, sy+0.05*k, 0.1*k, body, 1, opOver)
	c.rect(int(hx-0.23*k), int(sy+0.05*k), int(hx+0.23*k)+1, int(by), body, 1, opOver)
	c.rect(int(hx-0.05*k), int(hy), int(hx+0.05*k)+1, int(sy)+1, body, 1, opOver)
	c.disc(hx, hy, 0.115*k, mix(body, rim, 1), 1, opOver)
	c.disc(hx, hy+0.025*k, 0.115*k, body, 1, opOver)
	if p.kind != fanNone {
		ax := hx + p.side*0.17*k
		c.line(ax, sy+0.02*k, lx, ly+0.08*k, 0.08*k, body, 1, opOver)
	}
}

// encoreLight adds a point light at (x, y): a core of radius r and a soft
// halo around it. A light smaller than a pixel is split over the four
// pixels around it, so a far light sways smoothly instead of stepping.
func encoreLight(c *canvas, x, y, r float64, col [3]float32, a float32) {
	if a <= 0 {
		return
	}
	if r < 1 {
		x, y = x-0.5, y-0.5
		x0, y0 := math.Floor(x), math.Floor(y)
		fx, fy := float32(x-x0), float32(y-y0)
		a *= float32(0.5 + 0.5*r)
		ix, iy := int(x0), int(y0)
		c.plot(ix, iy, col, a*(1-fx)*(1-fy), opAdd)
		c.plot(ix+1, iy, col, a*fx*(1-fy), opAdd)
		c.plot(ix, iy+1, col, a*(1-fx)*fy, opAdd)
		c.plot(ix+1, iy+1, col, a*fx*fy, opAdd)
		return
	}
	R := r * 3
	r2, R2 := r*r, R*R
	x0, x1 := max(0, int(math.Floor(x-R))), min(c.w-1, int(math.Ceil(x+R)))
	if x0 > x1 {
		return
	}
	for py := max(0, int(math.Floor(y-R))); py <= min(c.h-1, int(math.Ceil(y+R))); py++ {
		dy := float64(py) + 0.5 - y
		row := c.px[(py*c.w+x0)*3 : (py*c.w+x1+1)*3]
		for i := 0; i < len(row); i += 3 {
			dx := float64(x0+i/3) + 0.5 - x
			d2 := dx*dx + dy*dy
			if d2 >= R2 {
				continue
			}
			q := 1 - d2/R2
			v := q * q * 0.4 // the halo
			if d2 < r2 {
				v += 1 - 0.5*d2/r2 // the core
			}
			k := a * float32(v)
			p := row[i : i+3 : i+3]
			p[0] += col[0] * k
			p[1] += col[1] * k
			p[2] += col[2] * k
		}
	}
}

// beams sweeps the stage lights out over the crowd from below the frame:
// three pairs, each lit by its own part of the spectrum, all flaring on a
// drop.
func (g *encoreScene) beams(c *canvas, s *sceneCtx) {
	fw, fh := float64(c.w), float64(c.h)
	oy := fh * 1.08
	for i := range 6 {
		pair, side := i/2, float64(i%2*2-1)
		v := 0.0
		for b := pair * 16; b < pair*16+16; b++ {
			v += s.Bands[b]
		}
		v /= 16
		in := (0.025 + 0.12*v*v + 0.15*s.crashEnv) * (1 + 0.5*s.beatEnv)
		if in < 0.01 {
			continue
		}
		ox := fw/2 + side*fw*(0.12+0.13*float64(pair))
		ang := side * (0.15 + 0.22*float64(pair) + 0.18*math.Sin(g.t*0.35+float64(i)*1.7))
		tan := math.Tan(ang)
		spread := 0.045 + 0.012*float64(pair)
		col := tint(s.grad(float64(pair)/2), 0.3)
		for y := 0; y < c.h; y++ {
			dist := oy - float64(y)
			xc := ox + dist*tan
			hw := dist * spread
			fall := float32(in * (0.4 + 0.6*float64(y)/fh))
			x0, x1 := max(0, int(xc-2*hw)), min(c.w-1, int(xc+2*hw))
			if x0 > x1 {
				continue
			}
			row := c.px[(y*c.w+x0)*3 : (y*c.w+x1+1)*3]
			for i := 0; i < len(row); i += 3 {
				d := (float64(x0+i/3) + 0.5 - xc) / hw
				q := float32(1 - d*d/4) // a bell, 0 at two half-widths
				a := fall * q * q
				p := row[i : i+3 : i+3]
				p[0] += col[0] * a
				p[1] += col[1] * a
				p[2] += col[2] * a
			}
		}
	}
}
