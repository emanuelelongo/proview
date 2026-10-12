package model3d

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
)

var BackgroundColor = color.RGBA{30, 32, 38, 255}

type Camera struct {
	Yaw, Pitch, Zoom, PanX, PanY float64
	Wireframe                    bool
}

func DefaultCamera() Camera { return Camera{Yaw: math.Pi / 4, Pitch: math.Pi / 6, Zoom: 1} }
func (c Camera) rotate(v Vector) Vector {
	sy, cy := math.Sincos(c.Yaw)
	sp, cp := math.Sincos(c.Pitch)
	x := cy*v.X - sy*v.Y
	y := sy*v.X + cy*v.Y
	return Vector{x, cp*v.Z - sp*y, sp*v.Z + cp*y}
}

// Render uses an orthographic camera and bounded triangle boxes. Mesh and camera
// are immutable for the duration of a frame; the returned image owns its pixels.
func Render(ctx context.Context, m *Mesh, c Camera, w, h int) (*image.RGBA, error) {
	if m == nil || len(m.Triangles) == 0 || w < 1 || h < 1 || w > 2400 || h > 2400 || int64(w)*int64(h) > 4_000_000 {
		return nil, fmt.Errorf("invalid model viewport")
	}
	if !finite(c.Yaw) || !finite(c.Pitch) || !finite(c.Zoom) || !finite(c.PanX) || !finite(c.PanY) {
		return nil, fmt.Errorf("invalid camera")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return renderFrame(ctx, m, c, w, h)
}

// RenderHighQuality uses four subpixel samples per output pixel.
func RenderHighQuality(ctx context.Context, m *Mesh, c Camera, w, h int) (*image.RGBA, error) {
	// Validate before allocating the larger buffers.
	if m == nil || len(m.Triangles) == 0 || w < 1 || h < 1 || w > 2400 || h > 2400 || int64(w)*int64(h) > 4_000_000 {
		return nil, fmt.Errorf("invalid model viewport")
	}
	if !finite(c.Yaw) || !finite(c.Pitch) || !finite(c.Zoom) || !finite(c.PanX) || !finite(c.PanY) {
		return nil, fmt.Errorf("invalid camera")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	high, err := renderFrame(ctx, m, c, w*2, h*2)
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := range w {
			i := high.PixOffset(x*2, y*2)
			j := dst.PixOffset(x, y)
			for channel := range 3 {
				dst.Pix[j+channel] = uint8((int(high.Pix[i+channel]) + int(high.Pix[i+4+channel]) + int(high.Pix[i+high.Stride+channel]) + int(high.Pix[i+high.Stride+4+channel]) + 2) / 4)
			}
			dst.Pix[j+3] = 255
		}
	}
	return dst, nil
}
func renderFrame(ctx context.Context, m *Mesh, c Camera, w, h int) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	depth := make([]float64, w*h)
	for i := range depth {
		depth[i] = math.Inf(-1)
		j := i * 4
		dst.Pix[j], dst.Pix[j+1], dst.Pix[j+2], dst.Pix[j+3] = BackgroundColor.R, BackgroundColor.G, BackgroundColor.B, BackgroundColor.A
	}
	scale := float64(min(w, h)) * .29 * math.Max(.05, math.Min(c.Zoom, 30))
	light := Vector{-.4, .6, 1}.Unit()
	project := func(t Triangle) [3]Vector {
		var result [3]Vector
		for i, v := range t.Vertices {
			v = c.rotate(m.normalized(v))
			result[i] = Vector{float64(w)/2 + v.X*scale + c.PanX*float64(w), float64(h)/2 - v.Y*scale + c.PanY*float64(h), v.Z}
		}
		return result
	}
	for i, t := range m.Triangles {
		if i%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		p := project(t)
		normal := c.rotate(m.normalized(t.Vertices[1]).Sub(m.normalized(t.Vertices[0])).Cross(m.normalized(t.Vertices[2]).Sub(m.normalized(t.Vertices[0])))).Unit()
		// Two-sided lighting accommodates common STL winding inconsistencies.
		intensity := surfaceLight(normal, light)
		var shades [3]float64
		for j, v := range t.Vertices {
			shades[j] = intensity
			if n, ok := m.normals[v]; ok {
				n = c.rotate(n)
				if math.Abs(n.Dot(normal)) >= .8 {
					shades[j] = surfaceLight(n, light)
				}
			}
		}
		if c.Wireframe {
			shades = [3]float64{-1, -1, -1}
		}
		if err := fillTriangle(ctx, dst, depth, p, shades); err != nil {
			return nil, err
		}
	}
	if c.Wireframe {
		for i, t := range m.Triangles {
			if i%128 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			p := project(t)
			for j := range 3 {
				line(dst, depth, p[j], p[(j+1)%3])
			}
		}
	}
	return dst, nil
}

// Orient lighting toward the viewer to tolerate reversed STL winding without
// reflecting the diffuse term across the shadow boundary.
func surfaceLight(normal, light Vector) float64 {
	if normal.Z < 0 {
		normal = normal.Scale(-1)
	}
	return .3 + .7*math.Max(0, normal.Dot(light))
}
func edge(a, b Vector, x, y float64) float64 { return (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X) }
func fillTriangle(ctx context.Context, dst *image.RGBA, depth []float64, p [3]Vector, shades [3]float64) error {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	area := edge(p[0], p[1], p[2].X, p[2].Y)
	if math.Abs(area) < 1e-12 {
		return nil
	}
	x0 := max(0, int(math.Floor(math.Min(p[0].X, math.Min(p[1].X, p[2].X)))))
	x1 := min(w-1, int(math.Ceil(math.Max(p[0].X, math.Max(p[1].X, p[2].X)))))
	y0 := max(0, int(math.Floor(math.Min(p[0].Y, math.Min(p[1].Y, p[2].Y)))))
	y1 := min(h-1, int(math.Ceil(math.Max(p[0].Y, math.Max(p[1].Y, p[2].Y)))))
	for y := y0; y <= y1; y++ {
		if y%32 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for x := x0; x <= x1; x++ {
			a := edge(p[1], p[2], float64(x)+.5, float64(y)+.5) / area
			b := edge(p[2], p[0], float64(x)+.5, float64(y)+.5) / area
			d := 1 - a - b
			if a < 0 || b < 0 || d < 0 {
				continue
			}
			z := a*p[0].Z + b*p[1].Z + d*p[2].Z
			i := y*w + x
			if z >= depth[i] {
				depth[i] = z
				j := i * 4
				shade := BackgroundColor
				if shades[0] >= 0 {
					value := uint8(math.Max(0, math.Min(255, 255*(a*shades[0]+b*shades[1]+d*shades[2]))))
					shade = color.RGBA{value, value, value, 255}
				}
				dst.Pix[j], dst.Pix[j+1], dst.Pix[j+2], dst.Pix[j+3] = shade.R, shade.G, shade.B, 255
			}
		}
	}
	return nil
}
func line(dst *image.RGBA, depth []float64, a, b Vector) {
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	dx, dy := b.X-a.X, b.Y-a.Y
	// Clip in parameter space before stepping, so distant edges remain bounded.
	lo, hi := 0., 1.
	for _, bound := range [][2]float64{{-dx, a.X}, {dx, float64(w-1) - a.X}, {-dy, a.Y}, {dy, float64(h-1) - a.Y}} {
		p, q := bound[0], bound[1]
		if p == 0 {
			if q < 0 {
				return
			}
			continue
		}
		r := q / p
		if p < 0 {
			lo = math.Max(lo, r)
		} else {
			hi = math.Min(hi, r)
		}
		if lo > hi {
			return
		}
	}
	steps := max(1, int(math.Ceil(math.Max(math.Abs(dx*(hi-lo)), math.Abs(dy*(hi-lo))))))
	for i := 0; i <= steps; i++ {
		t := lo + (hi-lo)*float64(i)/float64(steps)
		x, y := int(math.Round(a.X+t*dx)), int(math.Round(a.Y+t*dy))
		if x < 0 || x >= w || y < 0 || y >= h {
			continue
		}
		z := a.Z + t*(b.Z-a.Z)
		if z+0.025 < depth[y*w+x] {
			continue
		}
		j := (y*w + x) * 4
		dst.Pix[j], dst.Pix[j+1], dst.Pix[j+2] = 235, 235, 235
	}
}
