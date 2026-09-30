package termimg

import (
	"hash/fnv"
	"image"
	"image/color"
	"math"
)

// placeholderBars is the number of spectrum bars on a placeholder tile, and
// placeholderColors are the cliamp logo's bar colors from short to tall.
const placeholderBars = 8

var placeholderColors = []color.RGBA{
	{0x00, 0xff, 0x41, 0xff}, // green
	{0xff, 0xe0, 0x00, 0xff}, // yellow
	{0xff, 0x95, 0x00, 0xff}, // orange
	{0xff, 0x3b, 0x2f, 0xff}, // red
}

// PlaceholderArt draws a stand-in cover for an item without artwork: the
// cliamp logo's spectrum bars on a dark tile. The seed (usually the item's
// name) picks the bar heights and the background tint, so each item gets its
// own tile and keeps it across redraws.
func PlaceholderArt(seed string, side int) *image.RGBA {
	side = max(side, placeholderBars*4)
	h := fnv.New32a()
	h.Write([]byte(seed))
	state := h.Sum32() | 1 // xorshift state must be non-zero
	next := func() float64 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return float64(state%10000) / 10000
	}

	img := image.NewRGBA(image.Rect(0, 0, side, side))
	// Background: a dark shade of the seed's hue fading to near black.
	top := hsvColor(next()*360, 0.55, 0.24)
	bottom := color.RGBA{0x06, 0x06, 0x06, 0xff}
	for y := range side {
		c := lerpColor(top, bottom, float64(y)/float64(side-1))
		for x := range side {
			img.SetRGBA(x, y, c)
		}
	}

	// Bars: taller toward the center like the logo, each scaled by the seed.
	margin := side / 10
	slot := (side - 2*margin) / placeholderBars
	barW := max(1, slot*2/5)
	maxH := float64(side) * 0.72
	for i := range placeholderBars {
		envelope := 0.45 + 0.55*math.Sin(math.Pi*(float64(i)+0.5)/placeholderBars)
		frac := envelope * (0.5 + 0.5*next())
		barH := max(side/12, int(maxH*frac))
		// frac spans about 0.22-1; spread that over the palette, short to tall.
		c := placeholderColors[clampIndex(int((frac-0.22)/0.78*float64(len(placeholderColors))), len(placeholderColors))]
		x0 := margin + i*slot + (slot-barW)/2
		y0 := (side - barH) / 2
		for y := y0; y < y0+barH; y++ {
			bc := c
			if (y-y0)%4 == 3 { // the logo's scanline texture
				bc = lerpColor(c, color.RGBA{0, 0, 0, 0xff}, 0.35)
			}
			for x := x0; x < x0+barW; x++ {
				img.SetRGBA(x, y, bc)
			}
		}
	}
	return img
}

func clampIndex(i, n int) int { return min(max(i, 0), n-1) }

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	mix := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*t + 0.5) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xff}
}

// hsvColor converts hue (degrees), saturation and value (0-1) to RGB.
func hsvColor(h, s, v float64) color.RGBA {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	to := func(f float64) uint8 { return uint8((f+m)*255 + 0.5) }
	return color.RGBA{to(r), to(g), to(b), 0xff}
}
