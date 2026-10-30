// ocrprobe generates a deterministic evaluation corpus and measures the same
// document recognition path as the viewer. It does not change source documents.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"codeberg.org/go-pdf/fpdf"
	"emanuelelongo/proview/internal/ocr"
	"emanuelelongo/proview/pkg/document"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var lines = []string{
	"Proview riconosce il testo nelle immagini.",
	"Il caffè è pronto, più tardi apriamo il documento.",
	"Select these words and copy them to another application.",
	"Invoice number 12345. Total amount 250.00 EUR.",
	"La selezione segue le parole su più righe.",
	"Local recognition works without a network connection.",
}

type sample struct {
	name       string
	data       []byte
	expected   string
	diagnostic bool
}
type result struct {
	Name             string  `json:"name"`
	Diagnostic       bool    `json:"diagnostic"`
	Milliseconds     int64   `json:"milliseconds"`
	ExpectedWords    int     `json:"expected_words"`
	RecognizedWords  int     `json:"recognized_words"`
	WordAccuracy     float64 `json:"word_accuracy"`
	GoAllocatedBytes uint64  `json:"go_allocated_bytes"`
	GoHeapBytes      uint64  `json:"go_heap_bytes"`
	Error            string  `json:"error,omitempty"`
}

func main() {
	out := flag.String("out", "build/ocr-corpus", "Output corpus and report directory")
	file := flag.String("file", "", "Analyze one local image/PDF without copying it into the generated corpus")
	flag.Parse()
	var err error
	if *file != "" {
		err = probeFile(*file)
	} else {
		err = run(*out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	engine := &ocr.Tesseract{Path: os.Getenv("PROVIEW_TESSERACT_PATH")}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := engine.Check(ctx)
	cancel()
	if err != nil {
		return err
	}
	clean := render(1600, 900, 32, lines, false)
	dark := render(1000, 500, 16, lines, false)
	darkImage := image.NewRGBA(dark.Bounds())
	for y := 0; y < dark.Bounds().Dy(); y++ {
		for x := 0; x < dark.Bounds().Dx(); x++ {
			v := color.GrayModel.Convert(dark.At(x, y)).(color.Gray).Y
			darkImage.Set(x, y, color.Gray{Y: 255 - v})
		}
	}
	a4 := render(2480, 3508, 42, append(append(append([]string{}, lines...), lines...), lines...), false)
	columns := render(2400, 1400, 30, lines, true)
	blank := image.NewRGBA(image.Rect(0, 0, 1000, 700))
	draw.Draw(blank, blank.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	expected := strings.Join(lines, "\n")
	samples := []sample{
		{"screenshot.png", encode(clean), expected, false},
		{"dark-screenshot.png", encode(darkImage), expected, false},
		{"scan-a4.png", encode(a4), strings.Join(append(append(append([]string{}, lines...), lines...), lines...), "\n"), false},
		{"simulated-photo.png", encode(skew(clean)), expected, true},
		{"two-columns.png", encode(columns), columnExpected(), true},
		{"blank.png", encode(blank), "", false},
		{"native.pdf", pdf(nil, true), "Native PDF text stays selectable.\nPUBLIC SECRET KEEP", false},
		{"scanned.pdf", pdf(encode(a4), false), strings.Join(append(append(append([]string{}, lines...), lines...), lines...), "\n"), false},
		{"mixed.pdf", pdf(encode(a4), true), "Native PDF text stays selectable.\nPUBLIC SECRET KEEP\n" + strings.Join(append(append(append([]string{}, lines...), lines...), lines...), "\n"), true},
	}
	var results []result
	for _, s := range samples {
		if err := os.WriteFile(filepath.Join(dir, s.name), s.data, 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, s.name+".expected.txt"), []byte(s.expected), 0644); err != nil {
			return err
		}
		source, err := document.LoadBytes(s.name, s.name, s.data)
		if err != nil {
			return err
		}
		doc, err := document.New(source)
		if err != nil {
			source.Close()
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		layout, err := doc.RenderingSnapshot().SelectableText(ctx, 0, engine)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		cancel()
		doc.Close()
		actual := layout.Text(0, len(layout.Words)-1)
		r := result{Name: s.name, Diagnostic: s.diagnostic, Milliseconds: elapsed.Milliseconds(), ExpectedWords: len(strings.Fields(s.expected)), RecognizedWords: len(strings.Fields(actual)), WordAccuracy: accuracy(s.expected, actual), GoAllocatedBytes: after.TotalAlloc - before.TotalAlloc, GoHeapBytes: after.HeapAlloc}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
		if err := os.WriteFile(filepath.Join(dir, s.name+".actual.txt"), []byte(actual), 0644); err != nil {
			return err
		}
		fmt.Printf("%-22s %5d ms  accuracy %6.2f%%  words %d/%d\n", s.name, r.Milliseconds, 100*r.WordAccuracy, r.RecognizedWords, r.ExpectedWords)
	}
	models := map[string]string{}
	for _, lang := range []string{"eng", "ita"} {
		if b, err := os.ReadFile(filepath.Join(os.Getenv("TESSDATA_PREFIX"), lang+".traineddata")); err == nil {
			models[lang] = fmt.Sprintf("%x", sha256.Sum256(b))
		}
	}
	report := struct {
		Time        string
		Platform    string
		Go          string
		Engine      string
		ModelSHA256 map[string]string
		Results     []result
	}{time.Now().UTC().Format(time.RFC3339), runtime.GOOS + "/" + runtime.GOARCH, runtime.Version(), engine.Version, models, results}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), b, 0644); err != nil {
		return err
	}
	for _, r := range results {
		if r.Error != "" {
			return fmt.Errorf("recognition failed for %s: %s (report saved)", r.Name, r.Error)
		}
	}
	return nil
}
func render(w, h int, size float64, text []string, columns bool) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	defer face.Close()
	count := 1
	if columns {
		count = 2
	}
	for col := 0; col < count; col++ {
		for i, line := range text {
			d := font.Drawer{Dst: out, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(80+col*w/2, 100+i*int(size*1.8))}
			if columns {
				line = fmt.Sprintf("%d. %s", col*len(text)+i+1, line)
			}
			d.DrawString(line)
		}
	}
	return out
}
func skew(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(b)
	// A deterministic 4-degree slant and illumination gradient, not a real photograph.
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			sy := y - int(math.Tan(4*math.Pi/180)*float64(x-b.Dx()/2))
			c := color.Gray{Y: 255}
			if sy >= 0 && sy < b.Dy() {
				c = color.GrayModel.Convert(src.At(x, sy)).(color.Gray)
			}
			c.Y = uint8(float64(c.Y) * (.68 + .30*float64(x)/float64(b.Dx())))
			out.Set(x, y, c)
		}
	}
	return out
}
func encode(im image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		panic(err)
	}
	return b.Bytes()
}
func pdf(pngData []byte, native bool) []byte {
	f := fpdf.New("P", "mm", "A4", "")
	f.AddPage()
	if pngData != nil {
		options := fpdf.ImageOptions{ImageType: "PNG"}
		f.RegisterImageOptionsReader("scan", options, bytes.NewReader(pngData))
		f.ImageOptions("scan", 0, 35, 210, 250, false, options, 0, "")
	}
	if native {
		f.SetFont("Helvetica", "", 14)
		f.Text(10, 12, "Native PDF text stays selectable.")
		f.Text(10, 22, "PUBLIC SECRET KEEP")
	}
	var b bytes.Buffer
	if err := f.Output(&b); err != nil {
		panic(err)
	}
	return b.Bytes()
}
func accuracy(expected, actual string) float64 {
	a, b := strings.Fields(expected), strings.Fields(actual)
	if len(a) == 0 {
		if len(b) == 0 {
			return 1
		}
		return 0
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, word := range a {
		next := make([]int, len(b)+1)
		next[0] = i + 1
		for j, got := range b {
			cost := 0
			if word != got {
				cost = 1
			}
			next[j+1] = min(previous[j+1]+1, next[j]+1, previous[j]+cost)
		}
		previous = next
	}
	return math.Max(0, 1-float64(previous[len(b)])/float64(len(a)))
}

func columnExpected() string {
	var text []string
	for col := 0; col < 2; col++ {
		for i, line := range lines {
			text = append(text, fmt.Sprintf("%d. %s", col*len(lines)+i+1, line))
		}
	}
	return strings.Join(text, "\n")
}

// probeFile deliberately leaves private source files and recognized text out of
// the generated corpus/report. It prints aggregate measurements only.
func probeFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	source, err := document.Load(path, file)
	if err != nil {
		return err
	}
	defer source.Close()
	doc, err := document.New(source)
	if err != nil {
		return err
	}
	engine := ocr.NewEngine()
	for page := 0; page < doc.PageCount(); page++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		started := time.Now()
		layout, err := doc.RenderingSnapshot().SelectableText(ctx, page, engine)
		cancel()
		if err != nil {
			return err
		}
		confident := 0
		confidence := 0.0
		for _, word := range layout.Words {
			confidence += word.Confidence
			if word.Confidence >= 80 {
				confident++
			}
		}
		if len(layout.Words) > 0 {
			confidence /= float64(len(layout.Words))
		}
		fmt.Printf("page=%d words=%d confidence>=80=%d mean_confidence=%.1f elapsed=%s\n", page+1, len(layout.Words), confident, confidence, time.Since(started).Round(time.Millisecond))
	}
	return nil
}
