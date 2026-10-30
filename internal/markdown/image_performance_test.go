package markdown

import (
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkLoadMarkdownImage(b *testing.B) {
	dir := b.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	rng := rand.New(rand.NewSource(1))
	_, _ = rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	f, err := os.Create(filepath.Join(dir, "image.png"))
	if err != nil {
		b.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, err := Load("test.md", filepath.Join(dir, "test.md"), strings.NewReader("![image](image.png)"))
		if err != nil || doc.Images["image.png"] == nil {
			b.Fatalf("image not loaded: %v", err)
		}
	}
}

func TestLoadWithBudgetAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA64(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		budget int64
		want   bool
	}{{1, false}, {4096, true}} {
		doc, err := LoadWithBudget("test.md", filepath.Join(dir, "test.md"), strings.NewReader("![image](image.png)"), tc.budget, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got := doc.Images["image.png"] != nil; got != tc.want {
			t.Fatalf("budget %d loaded image = %v, want %v", tc.budget, got, tc.want)
		}
		before := doc.ImageBytes()
		if tc.want {
			if doc.ImageCount() != 1 || doc.AnimationCount() != 0 || !doc.HasDecodedImages() {
				t.Fatal("decoded Markdown resource counts are incorrect")
			}
			original := doc.Image("image.png")
			doc.TrimImages()
			if !doc.ImagesTrimmed() || doc.HasDecodedImages() || doc.Image("image.png") != nil || doc.Animation("image.png") != nil {
				t.Fatal("TrimImages retained decoded image resources")
			}
			doc.RestoreImages()
			restored := doc.Image("image.png")
			if doc.ImagesTrimmed() || !doc.HasDecodedImages() || doc.ImageCount() != 1 || restored == nil || restored.Bounds() != original.Bounds() || restored.At(3, 4) != original.At(3, 4) {
				t.Fatal("RestoreImages did not restore the local image")
			}
		}
		doc.Close()
		doc.Close()
		if doc.Images != nil || doc.Animations != nil || before != 0 && doc.ImageBytes() != 0 {
			t.Fatal("Close did not release decoded Markdown resources")
		}
	}
}

func TestMarkdownImageDecodeBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// RGBA64 exercises the largest supported decoded pixel representation.
	if err := png.Encode(f, image.NewRGBA64(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Document{directory: canonical}
	const cost = 16 * 16 * 8
	if img := doc.loadImage("image.png", cost-1); img != nil {
		t.Fatal("decoded image exceeding remaining budget")
	}
	if img := doc.loadImage("image.png", cost); img == nil {
		t.Fatal("image fitting budget rejected")
	}
	if err := os.WriteFile(path, []byte("invalid PNG"), 0600); err != nil {
		t.Fatal(err)
	}
	if img := doc.loadImage("image.png", cost); img != nil {
		t.Fatal("corrupt image accepted")
	}
}
