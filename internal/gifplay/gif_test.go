package gifplay

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func encoded(t *testing.T, loop int, disposal []byte) []byte {
	t.Helper()
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	frames := []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 3, 1), palette),
		image.NewPaletted(image.Rect(1, 0, 2, 1), palette),
		image.NewPaletted(image.Rect(2, 0, 3, 1), palette),
	}
	frames[0].SetColorIndex(0, 0, 1)
	frames[1].SetColorIndex(1, 0, 2)
	frames[2].SetColorIndex(2, 0, 3)
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: frames, Delay: []int{2, 3, 4}, Disposal: disposal, LoopCount: loop, Config: image.Config{ColorModel: palette, Width: 3, Height: 1}}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestGIFCompositionDisposalAndImmutableFrames(t *testing.T) {
	for _, disposal := range []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious} {
		a, err := Decode(encoded(t, -1, []byte{gif.DisposalNone, disposal, 0}), MaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		p := a.NewPlayer()
		first := p.Image()
		now := time.Unix(1, 0)
		if p.Advance(now) || p.Advance(now.Add(19*time.Millisecond)) {
			t.Fatal("advanced before delay")
		}
		if !p.Advance(now.Add(20 * time.Millisecond)) {
			t.Fatal("second frame missing")
		}
		second := p.Image()
		if color.NRGBAModel.Convert(second.At(1, 0)).(color.NRGBA).G != 255 {
			t.Fatal("offset frame not composed")
		}
		if !p.Advance(now.Add(50 * time.Millisecond)) {
			t.Fatal("third frame missing")
		}
		third := p.Image()
		middle := color.NRGBAModel.Convert(third.At(1, 0)).(color.NRGBA)
		if (disposal == gif.DisposalNone && middle.G != 255) || (disposal != gif.DisposalNone && middle.A != 0) {
			t.Fatalf("wrong disposal %d: %v", disposal, middle)
		}
		if color.NRGBAModel.Convert(first.At(1, 0)).(color.NRGBA).A != 0 || color.NRGBAModel.Convert(second.At(1, 0)).(color.NRGBA).G != 255 {
			t.Fatal("published frames mutated")
		}
		if p.Advance(now.Add(time.Second)) {
			t.Fatal("one-shot GIF repeated")
		}
	}
}

func TestGIFLoopCountsPauseAndLimits(t *testing.T) {
	for _, loop := range []int{-1, 0, 1, 2} {
		data := encoded(t, loop, nil)
		a, err := Decode(data, MaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(data, a.Bytes-1); err == nil {
			t.Fatal("allocation budget ignored")
		}
		for end := 0; end < len(data)-1; end++ {
			if _, err := Decode(data[:end], MaxBytes); err == nil {
				t.Fatalf("truncated GIF accepted at %d", end)
			}
		}
		p := a.NewPlayer()
		now := time.Unix(1, 0)
		p.Advance(now)
		p.Pause()
		now = now.Add(time.Hour)
		if p.Advance(now) {
			t.Fatal("pause skipped frames")
		}
		changes := 0
		for range 15 {
			now = now.Add(time.Second)
			if p.Advance(now) {
				changes++
			}
		}
		want := 15
		if loop == -1 {
			want = 2
		} else if loop > 0 {
			want = (loop+1)*3 - 1
		}
		if changes != want {
			t.Fatalf("loop=%d changes=%d want=%d", loop, changes, want)
		}
	}
}

func TestGIFRepositoryAnimationsFitSharedBudget(t *testing.T) {
	paths, err := filepath.Glob("../../docs/screenshots/*.gif")
	if err != nil || len(paths) == 0 {
		t.Fatal("missing repository GIFs")
	}
	remaining := MaxBytes
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		a, err := Decode(data, remaining)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		remaining -= a.Bytes
		if !a.Animated() {
			t.Fatalf("%s not animated", path)
		}
	}
}
