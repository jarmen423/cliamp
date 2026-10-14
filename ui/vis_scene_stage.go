package ui

// vis_scene_stage.go is Stage: a pixel-art band that plays along. Each drum
// hits on an onset in its own range (kick in the lows, snare in the mids,
// hi-hat in the highs, the crash on a drop). The bass strings shake with the
// low end and a dot on the neck marks the bass note. The keyboard lights the
// notes it hears and sends each new one floating up. In a full-screen view
// the band plays in front of the Ridgeline.

import "math"

// Stage boxes in art pixels: the bass rig, the drum kit and the keys, all
// 60 art pixels tall.
const (
	stageBassW = 100
	stageKitW  = 96
	stageKeysW = 108
	stageGap   = 6
	stageArtH  = 60
	stageTotal = stageBassW + stageKitW + stageKeysW + 2*stageGap
)

type stageNote struct {
	x, y, vy, life, ph float64
	col                [3]float32
}

type stageScene struct {
	backdrop ridgeScene
	notes    []stageNote
	held     [SceneKeys]float64 // seconds each key has been down, -1 when up
	sticks   [2]float64         // snare and hi-hat stick: 1 on the hit, falling back
	t        float64
}

func (st *stageScene) reset(w, h int) {
	st.backdrop.reset(w, h)
	st.notes = st.notes[:0]
	for k := range st.held {
		st.held[k] = -1
	}
}

// whiteOf maps a pitch class to its white key in the octave, -1 for black.
var whiteOf = [12]int{0, -1, 1, -1, 2, 3, -1, 4, -1, 5, -1, 6}

// stageKey returns the left edge and width of key k in keyboard art pixels,
// and whether it is white.
func stageKey(k int) (x, w int, isWhite bool) {
	o, pc := k/12, k%12
	if whiteOf[pc] >= 0 {
		return 2 + (o*7+whiteOf[pc])*5, 4, true
	}
	return 2 + (o*7+whiteOf[pc-1]+1)*5 - 2, 3, false
}

func (st *stageScene) draw(c *canvas, s *sceneCtx) {
	band := wide(c.w, c.h)
	fw, fh := float64(c.w), float64(c.h)
	u := max(1, min(int(fw/(stageTotal+8)), int(fh*0.9/stageArtH)))
	floorY := c.h - max(1, c.h/36)
	bh := 0
	if !band {
		u = max(1, min(int(fw/(stageTotal+8)), int(fh*0.3/stageArtH)))
		floorY = c.h - int(fh*26/640)
		bh = int(math.Round(fh * 0.62))
	}
	ox0 := (c.w - stageTotal*u) / 2
	oy := floorY - stageArtH*u
	oxB, oxK := ox0, ox0+(stageBassW+stageGap)*u
	oxP := oxK + (stageKitW+stageGap)*u

	if s.dt > 0 {
		st.step(s, band)
		if bh > 0 {
			st.backdrop.step(s, c.w, bh)
		}
	}

	c.rect(0, bh, c.w, c.h, [3]float32{}, 1, opOver) // the backdrop paints the rest
	if bh > 0 {
		st.backdrop.render(c, s, c.w, bh)
		for y := bh; y < floorY; y++ {
			t := float32(y-bh) / float32(max(1, floorY-bh))
			c.rect(0, y, c.w, y+1, shade(s.low, 0.05+0.11*t), 1, opOver)
		}
		for i := -10; i <= 10; i++ {
			c.line(fw/2+float64(i)*fw*0.05, float64(bh), fw/2+float64(i)*fw*0.11, float64(floorY), 1, s.low, 0.12, opOver)
		}
		c.rect(0, floorY, c.w, floorY+2, shade(s.low, 0.5), 1, opOver)
		c.rect(0, floorY+2, c.w, c.h, shade(s.low, 0.1), 1, opOver)
	} else {
		c.rect(0, floorY, c.w, floorY+1, shade(s.low, 0.4), 1, opOver)
	}

	// A spotlight per player, as bright as that player is busy.
	keysDown := 0
	for _, v := range s.Keys {
		if v > 0.55 {
			keysDown++
		}
	}
	lights := []struct {
		ox, w int
		col   [3]float32
		e     float64
	}{
		{oxB, stageBassW, s.low, s.bassLevel},
		{oxK, stageKitW, s.high, max(s.kickEnv, s.snareEnv*0.8, s.crashEnv)},
		{oxP, stageKeysW, s.mid, min(1, float64(keysDown)/4)},
	}
	for _, l := range lights {
		cx := float64(l.ox) + float64(l.w*u)/2
		spread := float64(l.w*u) * 0.65
		for y := 0; y < floorY; y++ {
			t := float64(y) / float64(floorY)
			hw := 4 + (spread-4)*t
			c.rect(int(cx-hw), y, int(cx+hw), y+1, l.col, float32((0.04+0.2*l.e)*t), opAdd)
		}
	}

	// PA stacks fill the strip's empty sides.
	if band && ox0/u >= 40 {
		for _, x := range []int{ox0/2 - 13*u, c.w - ox0/2 - 13*u} {
			p := art{c: c, ox: x, oy: oy, u: u}
			pump := 0
			if s.bassLevel > 0.5 {
				pump = 1
			}
			for _, y0 := range []float64{4, 32} {
				p.rect(0, y0, 26, 27, shade(s.low, 0.14), 1, opOver)
				p.rect(0, y0, 26, 1, shade(s.low, 0.4), 1, opOver)
				p.disc(13, y0+15, 8+pump, shade(s.low, 0.32), 1)
				p.disc(13, y0+15, 3, mix(shade(s.low, 0.3), s.low, float32(s.bassLevel)), 1)
				p.disc(13, y0+4, 1, shade(s.mid, 0.5), 1)
			}
		}
	}

	keys := art{c: c, ox: oxP, oy: oy, u: u}
	st.drawBass(art{c: c, ox: oxB, oy: oy, u: u}, s)
	st.drawKit(art{c: c, ox: oxK, oy: oy, u: u}, s)
	st.drawKeys(keys, s)
	for _, n := range st.notes {
		keys.rect(n.x, n.y, 2, 2, n.col, float32(n.life), opAdd)
		keys.rect(n.x, n.y+2, 2, 1, n.col, float32(n.life*0.35), opAdd)
	}
}

// step moves the sticks and the floating notes, and starts a note for each
// key that goes down (and slowly, more for keys held down).
func (st *stageScene) step(s *sceneCtx, band bool) {
	st.t += s.dt
	for i, hit := range []bool{s.snare, s.hat} {
		if hit {
			st.sticks[i] = 1
		} else {
			st.sticks[i] *= math.Pow(0.55, s.fr)
		}
	}
	for k, v := range s.Keys {
		if v <= 0.55 {
			st.held[k] = -1
			continue
		}
		life := 1.0
		if st.held[k] >= 0 {
			st.held[k] += s.dt
			if st.held[k] < 0.28 {
				continue
			}
			life = 0.45
		}
		st.held[k] = 0
		if len(st.notes) < 400 {
			x, w, _ := stageKey(k)
			ph := float64(k) * 1.7
			st.notes = append(st.notes, stageNote{
				x: float64(x) + float64(w)/2 - 1, y: 28,
				vy: -(0.5 + 0.5*math.Abs(math.Sin(ph+st.t))), life: life, ph: ph,
				col: s.grad(float64(k) / (SceneKeys - 1)),
			})
		}
	}
	decay := 0.011 // notes rise higher in a full-screen view
	if band {
		decay = 0.028
	}
	live := st.notes[:0]
	for _, n := range st.notes {
		n.y += n.vy * s.fr
		n.x += math.Sin(st.t*3.75+n.ph) * 0.15 * s.fr
		n.life -= decay * s.fr
		if n.life > 0 {
			live = append(live, n)
		}
	}
	st.notes = live
}

func (st *stageScene) drawBass(p art, s *sceneCtx) {
	bl := s.bassLevel
	p.rect(0, 4, 32, 8, shade(s.low, 0.2), 1, opOver)
	p.rect(0, 4, 32, 1, shade(s.low, 0.5), 1, opOver)
	for i := range 4 {
		p.rect(4+float64(i)*5, 7, 2, 2, shade(s.mid, 0.7), 1, opOver)
	}
	p.rect(27, 7, 2, 2, mix(shade(s.high, 0.3), s.high, float32(bl)), 1, opOver)
	p.rect(0, 13, 32, 47, shade(s.low, 0.16), 1, opOver)
	p.rect(0, 13, 32, 1, shade(s.low, 0.45), 1, opOver)
	sr := 11
	if bl > 0.55 {
		sr = 12
	}
	p.disc(16, 36, sr+1, shade(s.low, 0.42), 1)
	p.disc(16, 36, sr, shade(s.low, 0.26), 1)
	p.disc(16, 36, int(math.Round(float64(sr)*0.55)), shade(s.low, 0.2), 1)
	p.disc(16, 36, 3, mix(shade(s.mid, 0.4), s.mid, float32(bl)), 1)
	p.disc(16, 53, 2, shade(s.low, 0.5), 1)

	// The bass guitar: headstock, neck and frets, body, and four strings with
	// E at the bottom.
	p.rect(34, 25, 6, 12, shade(s.high, 0.35), 1, opOver)
	p.rect(40, 27, 46, 8, shade(s.mid, 0.25), 1, opOver)
	fretX := func(n int) float64 { return 40 + math.Round(46*(1-math.Pow(2, -float64(n)/12))*1.75) }
	for n := 1; n <= 12; n++ {
		p.rect(fretX(n), 27, 1, 8, shade(s.mid, 0.5), 1, opOver)
	}
	for _, n := range []int{3, 5, 7, 9} {
		p.px(math.Round((fretX(n)+fretX(n-1))/2), 30, shade(s.mid, 0.6))
	}
	for dy := -10; dy <= 10; dy++ {
		hw := math.Round(8 * math.Sqrt(max(0, 1-math.Pow(float64(dy)/10.5, 2))))
		if dy > 0 {
			hw++
		}
		col := shade(s.high, 0.55)
		if dy > -4 && dy < 4 {
			col = mix(col, shade(s.mid, 0.6), 0.5)
		}
		p.rect(90-hw, 31+float64(dy), 2*hw+1, 1, col, 1, opOver)
	}
	base := [4]int{28, 33, 38, 43} // E1 A1 D2 G2
	ys := [4]float64{34, 32, 30, 28}
	ns, fret := -1, 0
	if s.BassNote >= base[0] {
		for i := 3; i >= 0; i-- {
			if s.BassNote >= base[i] {
				ns = i
				break
			}
		}
		fret = min(12, s.BassNote-base[ns])
	}
	for i := range 4 {
		a := bl * 0.7
		if i == ns {
			a = bl * 2.6
		}
		col := mix(shade(s.low, 0.6), tint(s.low, 0.6), float32(min(1, a/2.5)))
		for x := 36.0; x <= 94; x++ {
			dy := math.Round(a * math.Sin(math.Pi*(x-36)/58) * math.Sin(s.T*47+float64(i)*1.7))
			p.px(x, ys[i]+dy, col)
		}
	}
	if ns >= 0 {
		fx := 40.0
		if fret > 0 {
			fx = math.Round((fretX(fret) + fretX(fret-1)) / 2)
		}
		p.rect(fx-1, ys[ns]-1, 3, 3, s.high, 0.35, opAdd)
		p.px(fx, ys[ns], tint(s.high, 0.5))
	}
}

func (st *stageScene) drawKit(p art, s *sceneCtx) {
	metal, brass := shade(s.low, 0.45), shade(s.mid, 0.8)
	head := tint(shade(s.low, 0.5), 0.5)

	// The crash wobbles after a hit.
	p.line(84, 12, 84, 59, metal, 1)
	tilt := math.Sin(st.t*26) * s.crashEnv * 4
	for i := -11.0; i <= 11; i++ {
		p.rect(84+i, 10+i/11*tilt, 1, 2, mix(brass, white, float32(s.crashEnv*0.6)), 1, opOver)
	}
	p.line(12, 22, 12, 59, metal, 1)
	p.rect(8, 59, 9, 1, metal, 1, opOver)

	// Rack tom, kick and floor tom. The kick swells, flashes and sends out a
	// ring on each hit.
	p.rect(44, 15, 16, 9, shade(s.high, 0.45), 1, opOver)
	p.rect(44, 15, 16, 1, head, 1, opOver)
	kr := 17
	if s.kickEnv > 0.5 {
		kr = 18
	}
	p.disc(52, 41, kr, shade(s.low, 0.55), 1)
	p.disc(52, 41, kr-2, mix(shade(s.low, 0.12), s.high, float32(s.kickEnv*0.45)), 1)
	p.disc(52, 41, 7, mix(shade(s.mid, 0.45), tint(s.mid, 0.4), float32(s.kickEnv)), 1)
	if s.kickEnv > 0.06 {
		p.ring(52, 41, float64(kr)+(1-s.kickEnv)*22, s.high, float32(s.kickEnv*0.8))
	}
	p.rect(72, 36, 18, 18, shade(s.high, 0.45), 1, opOver)
	p.rect(72, 36, 18, 1, head, 1, opOver)
	p.line(73, 54, 71, 59, metal, 1)
	p.line(89, 54, 91, 59, metal, 1)

	// The snare drops a pixel and its head flashes on a hit.
	sj := 0.0
	if s.snareEnv > 0.6 {
		sj = 1
	}
	p.line(27, 43, 27, 59, metal, 1)
	p.rect(23, 59, 9, 1, metal, 1, opOver)
	p.rect(18, 36+sj, 18, 7, shade(s.high, 0.55), 1, opOver)
	p.rect(18, 36+sj, 18, 1, mix(head, white, float32(s.snareEnv)), 1, opOver)

	// The hi-hat closes on a hit.
	p.rect(2, 23, 21, 2, brass, 1, opOver)
	top := 22 - math.Round(3*(1-s.hatEnv))
	p.rect(2, top, 21, 2, mix(brass, white, float32(s.hatEnv*0.5)), 1, opOver)
	p.rect(11, top-1, 3, 1, brass, 1, opOver)

	// Sticks snap down on the hit and rise back.
	stick := func(gx, gy, tx, ty, pose float64) {
		th := math.Atan2(ty-gy, tx-gx) + (1-pose)*1.0
		l := math.Hypot(tx-gx, ty-gy)
		ex, ey := gx+math.Cos(th)*l, gy+math.Sin(th)*l
		p.line(gx, gy, ex, ey, tint(s.mid, 0.55), 1)
		p.rect(ex-0.5, ey-0.5, 2, 2, tint(s.mid, 0.75), 1, opOver)
	}
	stick(38, 22, 26, 35+sj, st.sticks[0])
	stick(28, 9, 12, top-1, st.sticks[1])
}

func (st *stageScene) drawKeys(p art, s *sceneCtx) {
	leg := shade(s.low, 0.4)
	p.line(14, 46, 30, 59, leg, 1)
	p.line(30, 46, 14, 59, leg, 1)
	p.line(78, 46, 94, 59, leg, 1)
	p.line(94, 46, 78, 59, leg, 1)
	p.rect(0, 24, 108, 22, shade(s.low, 0.2), 1, opOver)
	p.rect(0, 24, 108, 5, shade(s.low, 0.3), 1, opOver)
	p.rect(4, 26, 2, 2, shade(s.mid, 0.7), 1, opOver)
	p.rect(9, 26, 2, 2, shade(s.mid, 0.7), 1, opOver)
	for pc := range 12 { // one light per pitch class
		v := max(s.Keys[pc], s.Keys[pc+12], s.Keys[pc+24])
		p.rect(44+float64(pc)*5, 26, 3, 1, mix(shade(s.mid, 0.3), s.mid, float32(v)), 1, opOver)
	}
	for _, whites := range []bool{true, false} {
		for k := range SceneKeys {
			x, w, isWhite := stageKey(k)
			if isWhite != whites {
				continue
			}
			v := s.Keys[k]
			down := 0.0
			if v > 0.55 {
				down = 1
			}
			switch {
			case isWhite && down > 0:
				p.rect(float64(x), 30+down, float64(w), 14, mix(tint(s.mid, 0.3), s.high, float32(v*0.5)), 1, opOver)
			case isWhite:
				p.rect(float64(x), 30, float64(w), 14, shade(tint(s.low, 0.6), 0.8), 1, opOver)
			case down > 0:
				p.rect(float64(x), 30+down, float64(w), 8, s.high, 1, opOver)
			default:
				p.rect(float64(x), 30, float64(w), 8, shade(s.low, 0.12), 1, opOver)
			}
		}
	}
}
