package signatures

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"math"
	"strings"

	images "emanuelelongo/proview/pkg/images"
	xdraw "golang.org/x/image/draw"
)

// Scan is an immutable, orientation-normalized source for signature acquisition.
type Scan struct{ Image image.Image }

type TraceOptions struct {
	Crop      image.Rectangle
	Intensity float64 // 0..100; higher includes lighter ink.
	Smooth    float64 // 0..100; higher simplifies and rounds contours.
}

func DecodeScan(data []byte) (Scan, error) {
	if len(data) == 0 || len(data) > MaxFileSize {
		return Scan{}, fmt.Errorf("signature image must be smaller than 16 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Scan{}, err
	}
	if format != "jpeg" && format != "png" {
		return Scan{}, fmt.Errorf("choose a JPEG or PNG image")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return Scan{}, fmt.Errorf("signature image exceeds 16 million pixels")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Scan{}, err
	}
	return Scan{Image: images.JPEGOrientation(data).Apply(img)}, nil
}

// Vectorize traces filled outlines rather than inferring pen movement. No source
// image is embedded in the SVG. Closed contour orientation preserves ink holes.
func (scan Scan) Vectorize(ctx context.Context, options TraceOptions) (Asset, error) {
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	if scan.Image == nil {
		return Asset{}, fmt.Errorf("missing signature image")
	}
	if !validTraceValue(options.Intensity) || !validTraceValue(options.Smooth) {
		return Asset{}, fmt.Errorf("signature controls must be between 0 and 100")
	}
	crop := options.Crop
	if crop.Empty() {
		crop = scan.Image.Bounds()
	}
	crop = crop.Intersect(scan.Image.Bounds())
	if crop.Empty() {
		return Asset{}, fmt.Errorf("select an area inside the image")
	}
	width, height := crop.Dx(), crop.Dy()
	scale := math.Min(1, 1600/float64(max(width, height)))
	width, height = max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
	input := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(input, input.Bounds(), scan.Image, crop, xdraw.Src, nil)
	lum := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return Asset{}, err
		}
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(input.At(x, y)).(color.NRGBA)
			l := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			// Composite transparent PNG pixels onto white before separating ink.
			lum[y*width+x] = uint8((l*int(c.A) + 255*(255-int(c.A))) / 255)
		}
	}
	// Local brightest paper compensates gradual shading. Separable sliding maxima
	// are linear in pixel count and retain the interior of thick strokes.
	radius := max(8, min(48, max(width, height)/24))
	paper := scanBackground(lum, width, height, radius)
	mask := make([]bool, len(lum))
	threshold := scanContrastThreshold(options.Intensity)
	for i, l := range lum {
		contrast := 255 * float64(int(paper[i])-int(l)) / float64(max(1, int(paper[i])))
		mask[i] = contrast >= threshold
	}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	// Only isolated single pixels are removed; dots and short pen marks survive.
	clean := append([]bool(nil), mask...)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if !mask[y*width+x] {
				continue
			}
			neighbors := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if (dx != 0 || dy != 0) && x+dx >= 0 && x+dx < width && y+dy >= 0 && y+dy < height && mask[(y+dy)*width+x+dx] {
						neighbors++
					}
				}
			}
			if neighbors == 0 {
				clean[y*width+x] = false
			}
		}
	}
	contours, bounds, err := traceContours(ctx, clean, width, height)
	if err != nil {
		return Asset{}, err
	}
	var path strings.Builder
	for _, contour := range contours {
		if err := ctx.Err(); err != nil {
			return Asset{}, err
		}
		if err := writeTracePath(ctx, &path, contour, options.Smooth); err != nil {
			return Asset{}, err
		}
	}
	data := []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%d %d %d %d"><path fill="black" fill-rule="nonzero" d="%s"/></svg>`, bounds.Min.X-1, bounds.Min.Y-1, bounds.Dx()+2, bounds.Dy()+2, path.String()))
	asset := Asset{Kind: SVG, Data: data}
	if err := ctx.Err(); err != nil {
		return Asset{}, err
	}
	if err := asset.Validate(); err != nil {
		return Asset{}, err
	}
	return asset, nil
}

// Preserve the previous midpoint (27.5) while extending both ends: stronger
// filtering at low sensitivity and very faint ink at high sensitivity.
func scanContrastThreshold(sensitivity float64) float64 {
	return 0.5 + 108*math.Pow(1-sensitivity/100, 2)
}

func validTraceValue(value float64) bool { return !math.IsNaN(value) && value >= 0 && value <= 100 }

func scanBackground(input []uint8, w, h, radius int) []uint8 {
	horizontal := make([]uint8, len(input))
	output := make([]uint8, len(input))
	line := make([]uint8, max(w, h))
	filtered := make([]uint8, len(line))
	for y := 0; y < h; y++ {
		maxScanLine(input[y*w:(y+1)*w], filtered[:w], radius)
		copy(horizontal[y*w:(y+1)*w], filtered[:w])
	}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			line[y] = horizontal[y*w+x]
		}
		maxScanLine(line[:h], filtered[:h], radius)
		for y := 0; y < h; y++ {
			output[y*w+x] = filtered[y]
		}
	}
	return output
}

func maxScanLine(input, output []uint8, radius int) {
	queue := make([]int, len(input))
	head, tail, next := 0, 0, 0
	for i := range input {
		for next < len(input) && next <= i+radius {
			for tail > head && input[queue[tail-1]] <= input[next] {
				tail--
			}
			queue[tail] = next
			tail++
			next++
		}
		for head < tail && queue[head] < i-radius {
			head++
		}
		output[i] = input[queue[head]]
	}
}

type traceEdge struct {
	start     image.Point
	direction int
	used      bool
}

var traceDirections = []image.Point{{X: 1}, {Y: 1}, {X: -1}, {Y: -1}}

func traceContours(ctx context.Context, mask []bool, w, h int) ([][]image.Point, image.Rectangle, error) {
	edges := make([]traceEdge, 0)
	outgoing := make(map[image.Point][4]int)
	bounds := image.Rectangle{}
	ink := func(x, y int) bool { return x >= 0 && y >= 0 && x < w && y < h && mask[y*w+x] }
	add := func(p image.Point, d int) {
		edges = append(edges, traceEdge{start: p, direction: d})
		list := outgoing[p]
		list[d] = len(edges)
		outgoing[p] = list
	}
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, bounds, err
		}
		for x := 0; x < w; x++ {
			if !ink(x, y) {
				continue
			}
			r := image.Rect(x, y, x+1, y+1)
			if bounds.Empty() {
				bounds = r
			} else {
				bounds = bounds.Union(r)
			}
			if !ink(x, y-1) {
				add(image.Pt(x, y), 0)
			}
			if !ink(x+1, y) {
				add(image.Pt(x+1, y), 1)
			}
			if !ink(x, y+1) {
				add(image.Pt(x+1, y+1), 2)
			}
			if !ink(x-1, y) {
				add(image.Pt(x, y+1), 3)
			}
		}
		if len(edges) > 200_000 {
			return nil, bounds, fmt.Errorf("too many details; crop the signature or reduce intensity")
		}
	}
	if len(edges) == 0 {
		return nil, bounds, fmt.Errorf("no ink found; increase intensity or select a different area")
	}
	var contours [][]image.Point
	for start := range edges {
		if edges[start].used {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, bounds, err
		}
		var contour []image.Point
		index := start
		for {
			edge := &edges[index]
			edge.used = true
			contour = append(contour, edge.start)
			end := edge.start.Add(traceDirections[edge.direction])
			if end == edges[start].start {
				break
			}
			candidates := outgoing[end]
			found := -1
			// Keep diagonal ink components separate by choosing the rightmost turn.
			for _, turn := range []int{1, 0, 3, 2} {
				candidate := candidates[(edge.direction+turn)%4] - 1
				if candidate >= 0 && !edges[candidate].used {
					found = candidate
					break
				}
			}
			if found < 0 {
				return nil, bounds, fmt.Errorf("cannot trace this signature contour")
			}
			index = found
		}
		contours = append(contours, contour)
	}
	return contours, bounds, nil
}

// Reuse the existing signature curve fitter with short, dense contour chunks.
// Dense samples prevent curves from cutting across long sparse segments; bounded
// chunks cap fitting work and let cancelled slider requests stop promptly.
func writeTracePath(ctx context.Context, out *strings.Builder, points []image.Point, smooth float64) error {
	if len(points) < 3 {
		return nil
	}
	compact := make([]image.Point, 0, len(points))
	for i, p := range points {
		prev, next := points[(i+len(points)-1)%len(points)], points[(i+1)%len(points)]
		a, b := p.Sub(prev), next.Sub(p)
		if a.X*b.Y != a.Y*b.X {
			compact = append(compact, p)
		}
	}
	if len(compact) < 3 {
		return nil
	}
	if smooth == 0 || len(points) <= 8 {
		for i, p := range compact {
			if i == 0 {
				fmt.Fprintf(out, "M%d %d", p.X, p.Y)
			} else {
				fmt.Fprintf(out, "L%d %d", p.X, p.Y)
			}
		}
	} else {
		dense := make([]Point, len(points)+1)
		for i, p := range points {
			// A small circular filter removes the staircase of pixel boundaries
			// before curve fitting. Tiny isolated marks keep their exact outlines.
			average := Point{}
			for offset := -2; offset <= 2; offset++ {
				q := points[(i+offset+len(points))%len(points)]
				weight := float64(3-int(math.Abs(float64(offset)))) / 9
				average.X += float64(q.X) * weight
				average.Y += float64(q.Y) * weight
			}
			blend := smooth / 100
			dense[i] = Point{X: float64(p.X)*(1-blend) + average.X*blend, Y: float64(p.Y)*(1-blend) + average.Y*blend}
		}
		dense[len(points)] = dense[0]
		fmt.Fprintf(out, "M%.2f %.2f", dense[0].X, dense[0].Y)
		for start := 0; start < len(points); {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+128, len(points))
			left := unit(sub(dense[(start+1)%len(points)], dense[(start+len(points)-1)%len(points)]))
			right := unit(sub(dense[(end+len(points)-1)%len(points)], dense[(end+1)%len(points)]))
			var curves []cubic
			tolerance := smooth / 100 * 0.85
			fit(dense[start:end+1], left, right, tolerance*tolerance, 0, &curves)
			for _, c := range curves {
				fmt.Fprintf(out, "C%.2f %.2f %.2f %.2f %.2f %.2f", c[1].X, c[1].Y, c[2].X, c[2].Y, c[3].X, c[3].Y)
			}
			start = end
		}
	}
	out.WriteString("Z")
	return nil
}
