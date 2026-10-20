package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"emanuelelongo/proview/internal/textselect"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestSkewEstimation(t *testing.T) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 25, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	src := image.NewRGBA(image.Rect(0, 0, 700, 400))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := font.Drawer{Dst: src, Src: image.NewUniform(color.Black), Face: face}
	for y := 60; y < 360; y += 45 {
		d.Dot = fixed.P(35, y)
		d.DrawString("Printed text remains selectable after rotation.")
	}
	for _, degrees := range []float64{-32, -18, 0, 15, 29} {
		var img image.Image = src
		if degrees != 0 {
			img, _ = rotateForOCR(src, degrees*math.Pi/180)
		}
		got, err := skewAngle(context.Background(), img)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(got*180/math.Pi+degrees) > 1 {
			t.Fatalf("rotation %.1f: correction %.2f", degrees, got*180/math.Pi)
		}
	}
	blank := image.NewUniform(color.White)
	bounded := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(bounded, bounded.Bounds(), blank, image.Point{}, draw.Src)
	if angle, err := skewAngle(context.Background(), bounded); err != nil || angle != 0 {
		t.Fatalf("blank: %v %v", angle, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := skewAngle(ctx, src); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestRotationPreservesCornersAndSelectionGeometry(t *testing.T) {
	src := image.NewRGBA(image.Rect(7, 11, 207, 111))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for _, rect := range []image.Rectangle{image.Rect(7, 11, 13, 17), image.Rect(201, 11, 207, 17), image.Rect(7, 105, 13, 111), image.Rect(201, 105, 207, 111)} {
		draw.Draw(src, rect, image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	for _, degrees := range []float64{-39, -21, 17, 40} {
		rotated, r := rotateForOCR(src, degrees*math.Pi/180)
		for _, p := range []textselect.Point{{X: 3, Y: 3}, {X: 197, Y: 3}, {X: 3, Y: 97}, {X: 197, Y: 97}} {
			x, y := r.cosine*p.X-r.sine*p.Y+r.tx, r.sine*p.X+r.cosine*p.Y+r.ty
			if x < 0 || y < 0 || x >= float64(rotated.Bounds().Dx()) || y >= float64(rotated.Bounds().Dy()) {
				t.Fatal("corner cropped")
			}
			rr, _, _, _ := rotated.At(int(x), int(y)).RGBA()
			if rr > 32768 {
				t.Fatal("corner pixels lost during rotation")
			}
		}
		want := textselect.Rect(.1, .2, .6, .5)
		quad := want
		for j, p := range quad {
			x, y := p.X*200, p.Y*100
			quad[j] = textselect.Point{X: (r.cosine*x - r.sine*y + r.tx) / float64(rotated.Bounds().Dx()), Y: (r.sine*x + r.cosine*y + r.ty) / float64(rotated.Bounds().Dy())}
		}
		characters := []textselect.Character{{Start: 0, End: 7, Quad: quad}}
		restored := r.restore(textselect.Layout{Width: float64(rotated.Bounds().Dx()), Height: float64(rotated.Bounds().Dy()), Words: []textselect.Word{{Text: "example", Quad: quad, Characters: characters}}})
		if len(restored.Words) != 1 || restored.WordAt(textselect.Point{X: .3, Y: .3}, false) != 0 || restored.Text(0, 0) != "example" {
			t.Fatal("selection not restored")
		}
		for j, p := range restored.Words[0].Quad {
			if math.Hypot(p.X-want[j].X, p.Y-want[j].Y) > 1e-9 {
				t.Fatal("incorrect inverse mapping")
			}
			if restored.Words[0].Characters[0].Quad[j] != p || characters[0].Quad[j] != quad[j] {
				t.Fatal("character mapping changed cached geometry or disagrees with word")
			}
		}
	}
}
