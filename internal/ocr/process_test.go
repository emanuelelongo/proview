//go:build !windows

package ocr

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slow-ocr")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := (&Tesseract{Path: path}).Check(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("subprocess did not terminate promptly")
	}
}

func TestRecognizeDarkInputPreservesRetryAndCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hocr-engine")
	// Check both passes request character boxes; emit a different uncovered word
	// for Otsu so the final layout proves the retry survived polarity normalization.
	script := `#!/bin/sh
case "$*" in
  *tessedit_create_hocr=1*hocr_char_boxes=1*) ;;
  *) exit 1 ;;
esac
cat >/dev/null
case "$*" in
  *thresholding_method=2*)
    echo '<div class="ocr_page"><span class="ocrx_word" title="bbox 10 10 20 20; x_wconf 50"><span class="ocrx_cinfo" title="x_bboxes 10 10 20 20">A</span></span></div>' ;;
  *thresholding_method=0*)
    echo '<div class="ocr_page"><span class="ocrx_word" title="bbox 30 10 40 20; x_wconf 95"><span class="ocrx_cinfo" title="x_bboxes 30 10 40 20">B</span></span></div>' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	engine := &Tesseract{Path: path, checked: true}
	layout, err := engine.Recognize(context.Background(), retryImage(color.Black))
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Words) != 2 || layout.Words[0].Text != "A" || layout.Words[1].Text != "B" {
		t.Fatalf("lost primary or supplemental word: %+v", layout)
	}
	for _, word := range layout.Words {
		if len(word.Characters) != 1 || word.Characters[0].Start != 0 || word.Characters[0].End != 1 {
			t.Fatalf("lost character coordinates: %+v", word)
		}
	}
}
