package cli

import (
	"image"
	"testing"
)

func TestFitImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	out := fitImage(src, 300, 300)
	b := out.Bounds()
	if b.Dx() > 300 || b.Dy() > 300 {
		t.Fatalf("not fit: %v", b)
	}
	if b.Dx() != 300 || b.Dy() != 150 {
		t.Fatalf("wrong aspect-preserving size: %v", b)
	}

	small := image.NewRGBA(image.Rect(0, 0, 50, 50))
	out2 := fitImage(small, 300, 300)
	if out2.Bounds() != small.Bounds() {
		t.Fatalf("should not upscale: %v", out2.Bounds())
	}
}
