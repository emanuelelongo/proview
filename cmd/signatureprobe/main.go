// signatureprobe evaluates local raster signature imports without opening the GUI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"emanuelelongo/proview/pkg/signatures"
)

func main() {
	file := flag.String("file", "", "JPEG or PNG source")
	out := flag.String("out", "build/signature-prototype/result", "output prefix")
	cropArg := flag.String("crop", "", "source crop: x,y,width,height")
	intensity := flag.Float64("intensity", 50, "ink intensity, 0..100")
	smooth := flag.Float64("smooth", 75, "smoothing, 0..100")
	flag.Parse()
	if err := probe(*file, *out, *cropArg, *intensity, *smooth); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probe(file, out, cropArg string, intensity, smooth float64) error {
	reader, err := os.Open(file)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(reader, signatures.MaxFileSize+1))
	closeErr := reader.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	scan, err := signatures.DecodeScan(data)
	if err != nil {
		return err
	}
	crop := scan.Image.Bounds()
	if cropArg != "" {
		pieces := strings.Split(cropArg, ",")
		if len(pieces) != 4 {
			return fmt.Errorf("crop must contain x,y,width,height")
		}
		var values [4]int
		for i, piece := range pieces {
			values[i], err = strconv.Atoi(piece)
			if err != nil {
				return err
			}
		}
		if values[2] <= 0 || values[3] <= 0 {
			return fmt.Errorf("crop width and height must be positive")
		}
		crop = image.Rect(values[0], values[1], values[0]+values[2], values[1]+values[3]).Intersect(crop)
		if crop.Empty() {
			return fmt.Errorf("crop is outside the source")
		}
	}
	started := time.Now()
	asset, err := scan.Vectorize(context.Background(), signatures.TraceOptions{Crop: crop, Intensity: intensity, Smooth: smooth})
	if err != nil {
		return err
	}
	elapsed := time.Since(started)
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(out+".svg", asset.Data, 0600); err != nil {
		return err
	}
	preview, err := asset.Render(1200)
	if err != nil {
		return err
	}
	if err := writePNG(out+".png", preview); err != nil {
		return err
	}
	white := image.NewRGBA(preview.Bounds())
	draw.Draw(white, white.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(white, white.Bounds(), preview, preview.Bounds().Min, draw.Over)
	if err := writePNG(out+"-preview.png", white); err != nil {
		return err
	}
	source := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Draw(source, source.Bounds(), scan.Image, crop.Min, draw.Src)
	if err := writePNG(out+"-source.png", source); err != nil {
		return err
	}
	pdf, err := asset.PDF()
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+".pdf", pdf, 0600); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"svg_bytes": len(asset.Data), "elapsed_ms": elapsed.Milliseconds(), "source_crop": crop, "output_prefix": out})
}
func writePNG(path string, img image.Image) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
