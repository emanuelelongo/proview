package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"testing"
	"time"

	"emanuelelongo/proview/internal/textselect"
)

func retryImage(background color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	return img
}
func retryWord(text string, x, y float64, confidence float64) textselect.Word {
	return textselect.Word{Text: text, Confidence: confidence, Quad: textselect.Rect(x, y, x+.08, y+.04)}
}
func retryLayout(words ...textselect.Word) textselect.Layout {
	return textselect.Layout{Width: 100, Height: 100, Words: words}
}

func TestRetryLeavesSuccessfulPrimaryAndLightPagesUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name       string
		background color.Color
		primary    textselect.Layout
	}{
		{"confident dark screenshot", color.Black, retryLayout(retryWord("correct", .1, .1, 95))},
		{"weak light scan", color.White, retryLayout(retryWord("weak", .1, .1, 40))},
		{"blank light page", color.White, retryLayout()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := recognizeWithRetry(context.Background(), retryImage(tc.background), func(_ context.Context, method int) (textselect.Layout, error) {
				calls++
				if method != 2 {
					t.Fatal("unexpected second pass")
				}
				return tc.primary, nil
			})
			if err != nil || calls != 1 || !reflect.DeepEqual(got, tc.primary) {
				t.Fatalf("primary changed: %+v %v", got, err)
			}
		})
	}
}

func TestRetrySupplementsWithoutReplacingAnyPrimaryWords(t *testing.T) {
	primary := retryLayout(retryWord("first", .1, .2, 60), retryWord("last", .4, .2, 95))
	before := append([]textselect.Word(nil), primary.Words...)
	secondary := retryLayout(
		retryWord("heading", .1, .05, 95),
		retryWord("replacement", .1, .2, 99),
		retryWord("middle", .25, .2, 95),
		retryWord("duplicate", .25, .2, 98),
		retryWord("last", .4, .2, 99),
		retryWord("noise", .6, .3, 70),
		retryWord("|", .7, .4, 99),
	)
	calls := []int{}
	got, err := recognizeWithRetry(context.Background(), retryImage(color.Black), func(_ context.Context, method int) (textselect.Layout, error) {
		calls = append(calls, method)
		if method == 2 {
			return primary, nil
		}
		return secondary, nil
	})
	if err != nil || !reflect.DeepEqual(calls, []int{2, 0}) {
		t.Fatalf("passes: %v %v", calls, err)
	}
	if len(got.Words) != 4 || got.Text(0, 3) != "heading\n\nfirst middle last" {
		t.Fatalf("unexpected supplement: %q", got.Text(0, len(got.Words)-1))
	}
	if !reflect.DeepEqual(got.Words[1], before[0]) || !reflect.DeepEqual(got.Words[3], before[1]) || !reflect.DeepEqual(primary.Words, before) {
		t.Fatal("modified primary text/geometry/confidence/order")
	}
	if got.Words[2].Block != primary.Words[0].Block || got.Words[2].Line != primary.Words[0].Line {
		t.Fatal("missing word was not joined to its line")
	}
}

func TestRetryFailuresKeepPrimaryAndCancellationDiscardsIt(t *testing.T) {
	primary := retryLayout(retryWord("keep", .1, .1, 50))
	for _, failure := range []error{errors.New("failed"), context.DeadlineExceeded} {
		got, err := recognizeWithRetry(context.Background(), retryImage(color.Black), func(_ context.Context, method int) (textselect.Layout, error) {
			if method == 2 {
				return primary, nil
			}
			return textselect.Layout{}, failure
		})
		if err != nil || !reflect.DeepEqual(got, primary) {
			t.Fatalf("lost primary on %v: %v", failure, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err := recognizeWithRetry(ctx, retryImage(color.Black), func(_ context.Context, method int) (textselect.Layout, error) {
		if method == 2 {
			return primary, nil
		}
		cancel()
		return retryLayout(retryWord("stale", .5, .1, 99)), nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled job published: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	got, err := recognizeWithRetry(ctx, retryImage(color.Black), func(_ context.Context, method int) (textselect.Layout, error) { calls++; return primary, nil })
	if calls != 1 || err != nil || !reflect.DeepEqual(got, primary) {
		t.Fatal("retry exhausted the remaining parent deadline")
	}
}

func TestSupplementGeometryAndReadingOrder(t *testing.T) {
	primary := retryLayout(retryWord("one", .1, .2, 50), retryWord("two", .1, .4, 95))
	primary.Words[1].Line = 1
	secondary := retryLayout(retryWord("between", .1, .3, 95), retryWord("below", .1, .6, 95))
	secondary.Words[1].Line = 1
	got, err := supplementWords(context.Background(), primary, secondary)
	if err != nil || len(got.Words) != 4 || !reflect.DeepEqual(got.Words[0], primary.Words[0]) || !reflect.DeepEqual(got.Words[2], primary.Words[1]) || got.Words[1].Text != "between" || got.Words[3].Text != "below" {
		t.Fatalf("reading order: %+v %v", got, err)
	}
	secondary.Width = 200
	got, err = supplementWords(context.Background(), primary, secondary)
	if err != nil || !reflect.DeepEqual(got, primary) {
		t.Fatal("accepted mismatched coordinate systems")
	}
	secondary = retryLayout(retryWord("outside", .99, .1, 99))
	got, err = supplementWords(context.Background(), primary, secondary)
	if err != nil || !reflect.DeepEqual(got, primary) {
		t.Fatal("accepted out-of-bounds word")
	}
}

func TestRetryEmptyPrimaryAndOriginalFailure(t *testing.T) {
	secondary := retryLayout(retryWord("recovered", .1, .1, 96))
	got, err := recognizeWithRetry(context.Background(), retryImage(color.Black), func(ctx context.Context, method int) (textselect.Layout, error) {
		if method == 2 {
			return retryLayout(), nil
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Fatal("second pass has no bounded deadline")
		}
		return secondary, nil
	})
	if err != nil || len(got.Words) != 1 || got.Words[0].Text != "recovered" {
		t.Fatalf("empty primary recovery: %+v %v", got, err)
	}
	failure := errors.New("primary failed")
	calls := 0
	_, err = recognizeWithRetry(context.Background(), retryImage(color.Black), func(context.Context, int) (textselect.Layout, error) {
		calls++
		return textselect.Layout{}, failure
	})
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatal("second pass masked an original recognition failure")
	}
}
