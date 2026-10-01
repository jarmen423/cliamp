package termimg

import (
	"bytes"
	"testing"
)

func TestPlaceholderArtIsStablePerSeed(t *testing.T) {
	a := PlaceholderArt("Top Tracks", 64)
	b := PlaceholderArt("Top Tracks", 64)
	c := PlaceholderArt("Recently Played", 64)
	if a.Rect.Dx() != 64 || a.Rect.Dy() != 64 {
		t.Fatalf("size = %v, want 64x64", a.Rect)
	}
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("same seed drew different tiles")
	}
	if bytes.Equal(a.Pix, c.Pix) {
		t.Error("different seeds drew identical tiles")
	}
}

// Every tile shows at least one bright logo-colored bar on a dark ground,
// even at sizes smaller than the bar count needs.
func TestPlaceholderArtDrawsBars(t *testing.T) {
	for _, side := range []int{1, 16, 256} {
		img := PlaceholderArt("x", side)
		bright := 0
		for i := 0; i < len(img.Pix); i += 4 {
			if max(img.Pix[i], img.Pix[i+1]) > 0xd0 {
				bright++
			}
		}
		if bright == 0 {
			t.Errorf("side %d: no bar pixels drawn", side)
		}
		if bg := img.RGBAAt(0, img.Rect.Dy()-1); max(bg.R, bg.G, bg.B) > 0x40 {
			t.Errorf("side %d: background corner %v is not dark", side, bg)
		}
	}
}
