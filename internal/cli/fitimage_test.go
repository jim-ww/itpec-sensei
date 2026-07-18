package cli

import (
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFitImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	out := fitImage(src, 300, 300)
	b := out.Bounds()
	assert.LessOrEqual(t, b.Dx(), 300, "not fit: %v", b)
	assert.LessOrEqual(t, b.Dy(), 300, "not fit: %v", b)
	assert.Equal(t, 300, b.Dx(), "wrong aspect-preserving size: %v", b)
	assert.Equal(t, 150, b.Dy(), "wrong aspect-preserving size: %v", b)

	small := image.NewRGBA(image.Rect(0, 0, 50, 50))
	out2 := fitImage(small, 300, 300)
	assert.Equal(t, small.Bounds(), out2.Bounds(), "should not upscale")
}
