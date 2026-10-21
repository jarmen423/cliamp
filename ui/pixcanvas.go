package ui

// pixcanvas.go is the small software rasterizer the pixel scenes draw with:
// a float RGB buffer with overwrite, alpha-blend and additive operations,
// and the few shapes the scenes need. Colors are linear 0..1 triples; the
// buffer clamps only when it is copied out.

import (
	"image"
	"math"
)

type blendOp int

const (
	opOver blendOp = iota // alpha blend
	opAdd                 // additive light
)

type canvas struct {
	w, h int
	px   []float32 // RGB

	cols []int // copyTo's source column per destination column, for colW
	colW int
}

// resize sets the size and reports whether it changed; a change clears it.
func (c *canvas) resize(w, h int) bool {
	if c.w == w && c.h == h {
		return false
	}
	c.w, c.h = w, h
	c.px = make([]float32, w*h*3)
	c.cols = nil
	return true
}

func (c *canvas) clear() { clear(c.px) }

// fade multiplies every pixel by k: the persistence of a trail buffer.
func (c *canvas) fade(k float32) {
	for i := range c.px {
		c.px[i] *= k
	}
}

// plot applies col at alpha a to pixel (x, y) with op.
func (c *canvas) plot(x, y int, col [3]float32, a float32, op blendOp) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || a <= 0 {
		return
	}
	o := (y*c.w + x) * 3
	p := c.px[o : o+3 : o+3]
	if op == opAdd {
		p[0] += col[0] * a
		p[1] += col[1] * a
		p[2] += col[2] * a
		return
	}
	if a >= 1 {
		p[0], p[1], p[2] = col[0], col[1], col[2]
		return
	}
	p[0] += (col[0] - p[0]) * a
	p[1] += (col[1] - p[1]) * a
	p[2] += (col[2] - p[2]) * a
}

// rect fills [x0, x1) x [y0, y1).
func (c *canvas) rect(x0, y0, x1, y1 int, col [3]float32, a float32, op blendOp) {
	x0, y0 = max(0, x0), max(0, y0)
	x1, y1 = min(c.w, x1), min(c.h, y1)
	if x0 >= x1 || y0 >= y1 || a <= 0 {
		return
	}
	r, g, b := col[0], col[1], col[2]
	if op == opAdd {
		r, g, b = r*a, g*a, b*a
	}
	for y := y0; y < y1; y++ {
		row := c.px[(y*c.w+x0)*3 : (y*c.w+x1)*3]
		for i := 0; i < len(row); i += 3 {
			p := row[i : i+3 : i+3]
			switch {
			case op == opAdd:
				p[0] += r
				p[1] += g
				p[2] += b
			case a >= 1:
				p[0], p[1], p[2] = r, g, b
			default:
				p[0] += (r - p[0]) * a
				p[1] += (g - p[1]) * a
				p[2] += (b - p[2]) * a
			}
		}
	}
}

// disc fills the pixels whose centers lie within r of (cx, cy).
func (c *canvas) disc(cx, cy, r float64, col [3]float32, a float32, op blendOp) {
	y0, y1 := int(math.Floor(cy-r)), int(math.Ceil(cy+r))
	for y := y0; y <= y1; y++ {
		dy := float64(y) + 0.5 - cy
		hw := r*r - dy*dy
		if hw < 0 {
			continue
		}
		s := math.Sqrt(hw)
		c.rect(int(math.Ceil(cx-s-0.5)), y, int(math.Floor(cx+s-0.5))+1, y+1, col, a, op)
	}
}

// line draws from (x0, y0) to (x1, y1), w pixels wide.
func (c *canvas) line(x0, y0, x1, y1, w float64, col [3]float32, a float32, op blendOp) {
	n := int(math.Ceil(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))))
	half := max(0, (w-1)/2)
	for i := 0; i <= n; i++ {
		t := 0.0
		if n > 0 {
			t = float64(i) / float64(n)
		}
		x, y := x0+(x1-x0)*t, y0+(y1-y0)*t
		c.rect(int(math.Floor(x-half)), int(math.Floor(y-half)), int(math.Floor(x+half))+1, int(math.Floor(y+half))+1, col, a, op)
	}
}

// arc draws an ellipse arc centered on (cx, cy) from angle a0 to a1
// (radians, clockwise on screen, within [0, 2π]), w pixels wide. alpha gives
// the opacity at each angle, so a ring can be brighter on one side. A thin
// arc is sampled along its length; a thick one is filled pixel by pixel over
// its bounding box, which costs far less than stamping squares.
func (c *canvas) arc(cx, cy, rx, ry, a0, a1, w float64, col [3]float32, alpha func(float64) float32, op blendOp) {
	if rx <= 0 || ry <= 0 {
		return
	}
	half := w / 2
	if w <= 3 {
		// About one sample per pixel of length, and one lane per pixel of
		// width along the normal, so additive light lands once per pixel.
		lanes := max(1, int(math.Round(w))) // at most 3
		steps := max(8, int(math.Ceil(math.Abs(a1-a0)*math.Max(rx, ry))))
		var last [3][2]int
		for i := range last {
			last[i] = [2]int{-1 << 30, 0}
		}
		for i := 0; i <= steps; i++ {
			th := a0 + (a1-a0)*float64(i)/float64(steps)
			cos, sin := math.Cos(th), math.Sin(th)
			x, y := cx+cos*rx, cy+sin*ry
			nx, ny := cos*ry, sin*rx
			nl := math.Hypot(nx, ny)
			a := alpha(th)
			for l := range lanes {
				o := float64(l) - float64(lanes-1)/2
				p := [2]int{int(math.Floor(x + nx/nl*o)), int(math.Floor(y + ny/nl*o))}
				if p != last[l] {
					c.plot(p[0], p[1], col, a, op)
					last[l] = p
				}
			}
		}
		return
	}
	// Rows outside the angle range are skipped for the half arcs the
	// scenes draw (0..π below the center, π..2π above it).
	yTop, yBot := cy-ry-half, cy+ry+half
	if a0 >= math.Pi {
		yBot = cy + half
	} else if a1 <= math.Pi {
		yTop = cy - half
	}
	minR := math.Min(rx, ry)
	for y := max(0, int(math.Floor(yTop))); y <= min(c.h-1, int(math.Ceil(yBot))); y++ {
		dy := float64(y) + 0.5 - cy
		for x := max(0, int(math.Floor(cx-rx-half))); x <= min(c.w-1, int(math.Ceil(cx+rx+half))); x++ {
			dx := float64(x) + 0.5 - cx
			// n is 1 on the ellipse; the distance from it along the ray from
			// the center is (n-1) times the ellipse radius on that ray.
			n := math.Hypot(dx/rx, dy/ry)
			if math.Abs(n-1)*minR > half {
				continue // no ray radius is shorter than minR: too far
			}
			if n == 0 {
				continue
			}
			cos, sin := dx/rx/n, dy/ry/n
			if math.Abs(n-1)*math.Hypot(cos*rx, sin*ry) > half {
				continue
			}
			th := math.Atan2(sin, cos)
			if th < 0 {
				th += 2 * math.Pi
			}
			if th < a0 || th > a1 {
				continue
			}
			c.plot(x, y, col, alpha(th), op)
		}
	}
}

// copyTo writes the canvas into dst, scaled to fit: nearest neighbor when
// it grows, a box filter when it shrinks.
func (c *canvas) copyTo(dst *image.RGBA) {
	dw, dh := dst.Rect.Dx(), dst.Rect.Dy()
	if dw <= 0 || dh <= 0 || c.w == 0 || c.h == 0 {
		return
	}
	if c.w <= dw && c.h <= dh {
		// Each source row is converted once; a destination row that maps to
		// the same source row as the one above is a copy of it.
		if c.colW != dw || len(c.cols) != dw {
			c.cols, c.colW = make([]int, dw), dw
			for x := range c.cols {
				c.cols[x] = x * c.w / dw * 3
			}
		}
		cols := c.cols
		prevSy := -1
		var prev []byte
		for y := range dh {
			row := dst.Pix[y*dst.Stride : y*dst.Stride+dw*4]
			sy := y * c.h / dh
			if sy == prevSy {
				copy(row, prev)
				continue
			}
			src := c.px[sy*c.w*3 : (sy+1)*c.w*3]
			for x, j := 0, 0; x < dw; x, j = x+1, j+4 {
				o := cols[x]
				row[j], row[j+1], row[j+2], row[j+3] = toByte(src[o]), toByte(src[o+1]), toByte(src[o+2]), 255
			}
			prevSy, prev = sy, row
		}
		return
	}
	x0s, x1s := make([]int, dw), make([]int, dw)
	for x := range dw {
		x0s[x] = x * c.w / dw
		x1s[x] = max(x0s[x]+1, (x+1)*c.w/dw)
	}
	for y := range dh {
		sy0, sy1 := y*c.h/dh, max(y*c.h/dh+1, (y+1)*c.h/dh)
		row := dst.Pix[y*dst.Stride:]
		for x := range dw {
			sx0, sx1 := x0s[x], x1s[x]
			var r, g, b float32
			for sy := sy0; sy < sy1; sy++ {
				o := (sy*c.w + sx0) * 3
				for sx := sx0; sx < sx1; sx++ {
					r += c.px[o]
					g += c.px[o+1]
					b += c.px[o+2]
					o += 3
				}
			}
			k := 1 / float32((sy1-sy0)*(sx1-sx0))
			j := x * 4
			row[j], row[j+1], row[j+2], row[j+3] = toByte(r*k), toByte(g*k), toByte(b*k), 255
		}
	}
}

// art draws pixel art on a canvas: one art pixel is u by u screen pixels,
// measured from the origin (ox, oy).
type art struct {
	c      *canvas
	ox, oy int
	u      int
}

func (p art) rect(x, y, w, h float64, col [3]float32, a float32, op blendOp) {
	x0, y0 := p.ox+int(math.Round(x))*p.u, p.oy+int(math.Round(y))*p.u
	w, h = math.Max(1, math.Round(w)), math.Max(1, math.Round(h))
	p.c.rect(x0, y0, x0+int(w)*p.u, y0+int(h)*p.u, col, a, op)
}

func (p art) px(x, y float64, col [3]float32) { p.rect(x, y, 1, 1, col, 1, opOver) }

// disc fills a pixel-art circle of radius r (art pixels).
func (p art) disc(cx, cy float64, r int, col [3]float32, a float32) {
	cx, cy = math.Round(cx), math.Round(cy)
	for dy := -r; dy <= r; dy++ {
		hw := math.Floor(math.Sqrt(float64(r*r-dy*dy)) + 0.35)
		p.rect(cx-hw, cy+float64(dy), 2*hw+1, 1, col, a, opOver)
	}
}

// line draws a pixel-art line t art pixels thick.
func (p art) line(x0, y0, x1, y1 float64, col [3]float32, t float64) {
	n := math.Max(1, math.Ceil(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))))
	o := (t - 1) / 2
	for i := 0.0; i <= n; i++ {
		p.rect(x0+(x1-x0)*i/n-o, y0+(y1-y0)*i/n-o, t, t, col, 1, opOver)
	}
}

// ring draws a pixel-art circle outline, added as light.
func (p art) ring(cx, cy, r float64, col [3]float32, a float32) {
	n := max(8, int(math.Ceil(r*7)))
	for i := range n {
		th := float64(i) / float64(n) * 2 * math.Pi
		p.rect(cx+math.Cos(th)*r, cy+math.Sin(th)*r, 1, 1, col, a, opAdd)
	}
}

// Color helpers for the scenes, on top of mix and gradient3.

func shade(c [3]float32, s float32) [3]float32 { return [3]float32{c[0] * s, c[1] * s, c[2] * s} }

func tint(c [3]float32, s float32) [3]float32 { return mix(c, [3]float32{1, 1, 1}, s) }

var white = [3]float32{1, 1, 1}
