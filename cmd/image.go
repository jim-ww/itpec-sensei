package cmd

import (
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"

	"github.com/mattn/go-isatty"
	sixel "github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
	"golang.org/x/sys/unix"

	"github.com/jim-ww/itpec-sensei/core"
)

func renderImage(c *core.Core, q *core.Question, viewer string, dark bool, lastExternalImage *string) error {
	if viewer == "xdg-open" {
		return openImageExternally(c, q, dark, lastExternalImage)
	}

	if !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Println("[image rendering skipped: not a terminal]")
		return nil
	}
	img, err := c.Bank.Image(q)
	if err != nil {
		return err
	}

	if maxW, maxH, ok := terminalPixelBudget(); ok {
		img = fitImage(img, maxW, maxH)
	}
	if dark {
		img = core.InvertImage(img)
	}

	enc := sixel.NewEncoder(os.Stdout)
	if err := enc.Encode(img); err != nil {
		return err
	}
	// Some terminals don't reliably move the cursor below the sixel image, so
	// force a couple of blank rows to keep the answer prompt from overlapping it.
	fmt.Println()
	fmt.Println()
	return nil
}

// openImageExternally hands the question's image to the user's xdg-open
// handler, for terminals without sixel support. When dark is false, the
// embedded PNG is copied to a temp file as-is; when true, it's decoded,
// inverted, and re-encoded, since there's no external-viewer equivalent of
// sixel's in-memory image.Image path. lastExternalImage is updated so the
// caller can later kill whatever process picked it up (see killExternalViewer).
func openImageExternally(c *core.Core, q *core.Question, dark bool, lastExternalImage *string) error {
	tmp, err := os.CreateTemp("", "itpec-sensei-*.png")
	if err != nil {
		return err
	}
	defer tmp.Close()

	if dark {
		img, err := c.Bank.Image(q)
		if err != nil {
			return err
		}
		if err := png.Encode(tmp, core.InvertImage(img)); err != nil {
			return err
		}
	} else {
		imagesFS, err := c.Bank.ImagesFS()
		if err != nil {
			return err
		}
		src, err := imagesFS.Open(q.ImageRelPath())
		if err != nil {
			return err
		}
		defer src.Close()
		if _, err := io.Copy(tmp, src); err != nil {
			return err
		}
	}

	if err := exec.Command("xdg-open", tmp.Name()).Start(); err != nil {
		return fmt.Errorf("xdg-open: %w", err)
	}
	fmt.Printf("[image opened externally: %s]\n", tmp.Name())
	*lastExternalImage = tmp.Name()
	return nil
}

// killExternalViewer best-effort kills whatever process opened the previous
// externally-viewed image (xdg-open itself has already exited, so we match on
// the temp file path in the target process's argv instead — this only catches
// viewers that take the path as a literal argument, e.g. feh/eog/sxiv, not
// browser- or portal-based handlers). No-op if nothing was opened.
func killExternalViewer(lastExternalImage *string) {
	if *lastExternalImage == "" {
		return
	}
	_ = exec.Command("pkill", "-f", *lastExternalImage).Run()
	_ = os.Remove(*lastExternalImage)
	*lastExternalImage = ""
}

// terminalPixelBudget returns the usable pixel area of the controlling terminal,
// leaving a couple of text rows free for the prompt/feedback printed around the
// image. Returns ok=false if the terminal doesn't report pixel dimensions (e.g.
// some terminal emulators leave ws_xpixel/ws_ypixel at 0).
func terminalPixelBudget() (w, h int, ok bool) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	cellHeight := float64(ws.Ypixel) / float64(ws.Row)
	const reservedRows = 3 // room for the question header/prompt lines
	budgetH := float64(ws.Ypixel) - cellHeight*reservedRows
	if budgetH < cellHeight {
		budgetH = float64(ws.Ypixel)
	}
	return int(ws.Xpixel), int(budgetH), true
}

// fitImage scales img down (never up) to fit within maxW x maxH, preserving
// aspect ratio.
func fitImage(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 || (srcW <= maxW && srcH <= maxH) {
		return img
	}

	scale := min(float64(maxW)/float64(srcW), float64(maxH)/float64(srcH))
	dstW := max(1, int(float64(srcW)*scale))
	dstH := max(1, int(float64(srcH)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}
