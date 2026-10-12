package ocr

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func testRuntime(t *testing.T, mutate func(map[string][]byte, *runtimeManifest)) []byte {
	t.Helper()
	files := map[string][]byte{"bin/tesseract.exe": []byte("test helper"), "tessdata/eng.traineddata": []byte("english"), "tessdata/ita.traineddata": []byte("italian")}
	manifest := runtimeManifest{OS: "windows", Arch: "amd64", Executable: "bin/tesseract.exe", Files: map[string]string{}}
	for name, data := range files {
		sum := sha256.Sum256(data)
		manifest.Files[name] = hex.EncodeToString(sum[:])
	}
	if mutate != nil {
		mutate(files, &manifest)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = data
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, data := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
func TestRuntimeExtractionConcurrentAndRepair(t *testing.T) {
	payload := testRuntime(t, nil)
	cache := filepath.Join(t.TempDir(), "cache with spaces à 東京")
	var wg sync.WaitGroup
	paths := make(chan string, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			path, err := unpackRuntime(context.Background(), payload, cache, "windows", "amd64")
			paths <- path
			failures <- err
		})
	}
	wg.Wait()
	close(paths)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for path := range paths {
		if first == "" {
			first = path
		}
		if path != first {
			t.Fatal("concurrent extraction failed to reuse publication")
		}
	}
	helper := filepath.Join(first, "bin", "tesseract.exe")
	if err := os.WriteFile(helper, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	repaired, err := unpackRuntime(context.Background(), payload, cache, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repaired, "bin", "tesseract.exe"))
	if err != nil || string(data) != "test helper" {
		t.Fatal("damaged cache was trusted")
	}
	// The original directory may still be used by another application process.
	if data, _ := os.ReadFile(helper); string(data) != "damaged" {
		t.Fatal("active cache was overwritten")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = unpackRuntime(ctx, payload, cache, "windows", "amd64"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestRuntimeRejectsInvalidPayload(t *testing.T) {
	for _, mutate := range []func(map[string][]byte, *runtimeManifest){
		func(f map[string][]byte, m *runtimeManifest) { m.Arch = "arm64" },
		func(f map[string][]byte, m *runtimeManifest) { f["../outside"] = []byte("bad") },
		func(f map[string][]byte, m *runtimeManifest) { f["bin/tesseract.exe"] = []byte("wrong checksum") },
		func(f map[string][]byte, m *runtimeManifest) { delete(f, "tessdata/ita.traineddata") },
	} {
		root := t.TempDir()
		cache := filepath.Join(root, "cache")
		if _, err := unpackRuntime(context.Background(), testRuntime(t, mutate), cache, "windows", "amd64"); err == nil {
			t.Fatal("invalid archive accepted")
		}
		if _, err := os.Stat(filepath.Join(root, "outside")); !os.IsNotExist(err) {
			t.Fatal("archive escaped cache")
		}
		entries, _ := os.ReadDir(cache)
		if len(entries) != 0 {
			t.Fatal("failed extraction left runtime files")
		}
	}
}
func TestBundledRecognitionWithoutSystemEngine(t *testing.T) {
	if len(runtimePayload) == 0 || os.Getenv("PROVIEW_OCR_SMOKE") != "1" {
		t.Skip("requires packaged OCR smoke check")
	}
	// Hide external engines/models; the exact same factory is used by the GUI.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TESSDATA_PREFIX", t.TempDir())
	t.Setenv("PROVIEW_TESSERACT_PATH", "")
	t.Setenv("PROVIEW_OCR", "")
	if runtime.GOOS == "windows" {
		t.Setenv("LocalAppData", filepath.Join(t.TempDir(), "OCR cache à 東京"))
	} else if runtime.GOOS == "linux" {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
	}
	if !Enabled() {
		t.Fatal("packaged OCR is not enabled")
	}
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 32, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 220))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: face, Dot: fixed.P(40, 90)}
	drawer.DrawString("Proview recognizes printed text.")
	drawer.Dot = fixed.P(40, 150)
	drawer.DrawString("Il testo italiano è selezionabile.")
	layout, err := NewEngine().Recognize(context.Background(), img)
	if err != nil {
		t.Fatal(err)
	}
	text := layout.Text(0, len(layout.Words)-1)
	if !strings.Contains(text, "Proview recognizes printed text.") || !strings.Contains(text, "Il testo italiano è selezionabile.") {
		t.Fatalf("bundled OCR smoke failed: %q", text)
	}
}
