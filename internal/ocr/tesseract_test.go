package ocr

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const header = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n"

func TestParseTSV(t *testing.T) {
	data := header + "1\t1\t0\t0\t0\t0\t0\t0\t100\t100\t-1\t\n" +
		"5\t1\t1\t1\t1\t1\t10\t10\t20\t10\t97.5\tCaffè\n" +
		"5\t1\t1\t2\t1\t1\t10\t40\t20\t10\t88\tpiù\n"
	l, err := ParseTSV(strings.NewReader(data), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Words) != 2 || l.Text(0, 1) != "Caffè\npiù" || l.Words[0].Confidence != 97.5 {
		t.Fatalf("%+v", l)
	}
	for _, bad := range []string{"garbage", header + "bad", header + "5\t1\t1\t1\t1\t1\t90\t10\t20\t10\t90\tbad\n"} {
		if _, err := ParseTSV(strings.NewReader(bad), 100, 100); err == nil {
			t.Fatal("accepted invalid TSV")
		}
	}
}
func TestMissingEngineAndCancellation(t *testing.T) {
	engine := &Tesseract{Path: t.TempDir() + "/missing-tesseract"}
	var setup *SetupError
	if err := engine.Check(context.Background()); !errors.As(err, &setup) {
		t.Fatalf("setup error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine = &Tesseract{}
	if err := engine.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
