package ui

import "math"

// bandOnset finds a sudden rise across a range of bands (a drum hit),
// judged against how much those bands usually rise in this song. from and
// to are fractions of the band count; gap counts analysis ticks.
type bandOnset struct {
	from, to   float64
	k, floor   float64
	gap, since int
	mean, dev  float64
}

func (o *bandOnset) reset() {
	o.mean, o.dev, o.since = 0.03, 0.02, o.gap
}

// step reports a hit and its strength, 1 at the threshold.
func (o *bandOnset) step(bands, prev []float64) (bool, float64) {
	n := len(bands)
	flux := 0.0
	for i := int(o.from * float64(n)); i < int(o.to*float64(n)) && i < len(prev); i++ {
		flux += max(0, bands[i]-prev[i])
	}
	threshold := max(o.mean+o.k*o.dev, o.floor)
	hit := flux > threshold && o.since >= o.gap
	o.mean = o.mean*0.92 + flux*0.08
	o.dev = o.dev*0.92 + math.Abs(flux-o.mean)*0.08
	if hit {
		o.since = 0
	} else {
		o.since++
	}
	return hit, flux / threshold
}
