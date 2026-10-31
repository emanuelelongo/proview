// Package gifplay decodes bounded GIF animations and composites their frames.
package gifplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"time"
)

const MaxBytes int64 = 128 << 20

type Animation struct {
	data  *gif.GIF
	Bytes int64
}

// Decode checks allocation costs before DecodeAll can allocate frame buffers.
func Decode(data []byte, budget int64) (*Animation, error) {
	cost, err := inspect(data, min(budget, MaxBytes))
	if err != nil {
		return nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &Animation{data: g, Bytes: cost}, nil
}

func inspect(data []byte, budget int64) (int64, error) {
	invalid := errors.New("invalid GIF or animation exceeds decoding limits")
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return 0, invalid
	}
	w, h := int(binary.LittleEndian.Uint16(data[6:])), int(binary.LittleEndian.Uint16(data[8:]))
	pixels := int64(w) * int64(h)
	if pixels == 0 || pixels > 8_000_000 {
		return 0, invalid
	}
	// Private composite, disposal snapshot and published image.
	cost := pixels * 12
	i := 13
	if data[10]&128 != 0 {
		i += 3 << ((data[10] & 7) + 1)
	}
	frames := 0
	for i < len(data) {
		tag := data[i]
		i++
		switch tag {
		case 0x3b:
			if frames == 0 || cost > budget {
				return 0, invalid
			}
			return cost, nil
		case 0x21:
			i++ // Extension label, followed by sub-blocks.
		case 0x2c:
			if i+9 > len(data) {
				return 0, invalid
			}
			x, y := int(binary.LittleEndian.Uint16(data[i:])), int(binary.LittleEndian.Uint16(data[i+2:]))
			fw, fh := int(binary.LittleEndian.Uint16(data[i+4:])), int(binary.LittleEndian.Uint16(data[i+6:]))
			packed := data[i+8]
			i += 9
			frames++
			cost += int64(fw)*int64(fh) + 8192
			if fw == 0 || fh == 0 || x+fw > w || y+fh > h || frames > 2000 || cost > budget {
				return 0, invalid
			}
			if packed&128 != 0 {
				i += 3 << ((packed & 7) + 1)
			}
			i++ // LZW code size.
		default:
			return 0, invalid
		}
		for {
			if i >= len(data) {
				return 0, invalid
			}
			n := int(data[i])
			i++
			if n == 0 {
				break
			}
			i += n
		}
	}
	return 0, invalid
}

func (a *Animation) Animated() bool { return a != nil && len(a.data.Image) > 1 }
func (a *Animation) NewPlayer() *Player {
	p := &Player{animation: a, canvas: image.NewNRGBA(image.Rect(0, 0, a.data.Config.Width, a.data.Config.Height))}
	p.reset()
	return p
}

// Player belongs to one view. Published frames never share its mutable canvas.
type Player struct {
	animation        *Animation
	canvas, previous *image.NRGBA
	published        *frameImage
	index, loops     int
	deadline         time.Time
	finished         bool
}

type frameImage struct{ pixels *image.NRGBA }

func (i *frameImage) ColorModel() color.Model { return color.NRGBAModel }
func (i *frameImage) Bounds() image.Rectangle { return i.pixels.Bounds() }
func (i *frameImage) At(x, y int) color.Color { return i.pixels.At(x, y) }

func (p *Player) background() image.Image {
	g := p.animation.data
	var c color.Color = color.Transparent
	if palette, ok := g.Config.ColorModel.(color.Palette); ok && int(g.BackgroundIndex) < len(palette) {
		c = palette[g.BackgroundIndex]
	}
	for _, c0 := range g.Image[0].Palette {
		if _, _, _, a := c0.RGBA(); a == 0 {
			c = color.Transparent
			break
		}
	}
	return image.NewUniform(c)
}
func (p *Player) reset() {
	draw.Draw(p.canvas, p.canvas.Bounds(), p.background(), image.Point{}, draw.Src)
	p.index = 0
	p.published = nil
	p.paint()
}
func (p *Player) disposal() byte {
	if p.index < len(p.animation.data.Disposal) {
		return p.animation.data.Disposal[p.index]
	}
	return 0
}
func (p *Player) paint() {
	if p.disposal() == gif.DisposalPrevious {
		p.previous = clone(p.canvas)
	} else {
		p.previous = nil
	}
	f := p.animation.data.Image[p.index]
	draw.Draw(p.canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
}
func clone(src *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}
func (p *Player) Image() image.Image {
	if p.published == nil {
		p.published = &frameImage{pixels: clone(p.canvas)}
	}
	return p.published
}
func (p *Player) delay() time.Duration {
	d := p.animation.data.Delay[p.index]
	if d < 2 {
		d = 10
	}
	return time.Duration(d) * 10 * time.Millisecond
}

// Pause retains the current frame and restarts its delay on resumption.
func (p *Player) Pause()         { p.deadline = time.Time{} }
func (p *Player) Finished() bool { return p.finished }
func (p *Player) Advance(now time.Time) bool {
	if p.finished {
		return false
	}
	if p.deadline.IsZero() {
		p.deadline = now.Add(p.delay())
		return false
	}
	if now.Before(p.deadline) {
		return false
	}
	// Published snapshots are independent of the compositing canvas. Invalidate
	// the reusable snapshot only when the next frame is actually changed.
	p.published = nil
	g := p.animation.data
	if p.index+1 == len(g.Image) {
		if g.LoopCount == -1 || (g.LoopCount > 0 && p.loops >= g.LoopCount) {
			p.finished = true
			return false
		}
		p.loops++
		p.reset()
	} else {
		switch p.disposal() {
		case gif.DisposalBackground:
			draw.Draw(p.canvas, g.Image[p.index].Bounds(), p.background(), image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if p.previous != nil {
				copy(p.canvas.Pix, p.previous.Pix)
			}
		}
		p.index++
		p.paint()
	}
	p.deadline = now.Add(p.delay())
	return true
}
