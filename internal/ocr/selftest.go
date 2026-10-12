package ocr

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// SelfTest verifies the packaged runtime through the same path as the viewer.
// Release checks can run it without a display server or any private document.
func SelfTest(ctx context.Context) error {
	if len(runtimePayload) == 0 {
		return fmt.Errorf("this build does not include the OCR runtime")
	}
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return err
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 32, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 220))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(40, 90)}
	drawer.DrawString("Proview recognizes printed text.")
	drawer.Dot = fixed.P(40, 150)
	drawer.DrawString("Il testo italiano è selezionabile.")
	// Explicit developer overrides must not hide a broken release payload.
	layout, err := (&packagedEngine{}).Recognize(ctx, img)
	if err != nil {
		return err
	}
	text := layout.Text(0, len(layout.Words)-1)
	if !strings.Contains(text, "Proview recognizes printed text.") || !strings.Contains(text, "Il testo italiano è selezionabile.") {
		return fmt.Errorf("bundled OCR did not recognize the English and Italian fixture")
	}
	return nil
}
