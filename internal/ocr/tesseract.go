// Package ocr provides the optional, local OCR backend used by the prototype.
package ocr

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"emanuelelongo/proview/internal/textselect"
)

type Engine interface {
	Recognize(context.Context, image.Image) (textselect.Layout, error)
}
type SetupError struct{ Err error }

func (e *SetupError) Error() string { return "OCR setup: " + e.Err.Error() }
func (e *SetupError) Unwrap() error { return e.Err }

type Tesseract struct {
	Path     string
	DataDir  string
	mu       sync.Mutex
	checked  bool
	setupErr error
	Version  string
}

func (t *Tesseract) command(ctx context.Context, args ...string) *exec.Cmd {
	path := t.Path
	if path == "" {
		path = "tesseract"
	}
	if t.DataDir != "" {
		// Keep model arguments ASCII even when the Windows user cache path
		// contains characters outside the helper CRT's active code page.
		args = append([]string{"--tessdata-dir", "."}, args...)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if t.DataDir != "" {
		cmd.Dir = t.DataDir
	}
	hideConsole(cmd)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "OMP_THREAD_LIMIT=") || (t.DataDir != "" && strings.HasPrefix(value, "TESSDATA_PREFIX=")) {
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "OMP_THREAD_LIMIT=1")
	return cmd
}
func (t *Tesseract) Check(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.checked {
		return t.setupErr
	}
	version, err := t.command(ctx, "--version").Output()
	if err == nil {
		t.Version = strings.SplitN(string(version), "\n", 2)[0]
		if !strings.HasPrefix(t.Version, "tesseract 5.") {
			err = fmt.Errorf("Tesseract 5 is required (found %s)", t.Version)
		}
	}
	if err == nil {
		var langs []byte
		langs, err = t.command(ctx, "--list-langs").Output()
		if err == nil {
			have := map[string]bool{}
			for _, s := range strings.Fields(string(langs)) {
				have[s] = true
			}
			if !have["ita"] || !have["eng"] {
				err = errors.New("install the ita and eng Tesseract language models")
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	t.checked = true
	if err != nil {
		t.setupErr = &SetupError{err}
	}
	return t.setupErr
}
func (t *Tesseract) Recognize(ctx context.Context, img image.Image) (textselect.Layout, error) {
	if err := t.Check(ctx); err != nil {
		return textselect.Layout{}, err
	}
	angle, err := skewAngle(ctx, img)
	if err != nil {
		return textselect.Layout{}, err
	}
	var correction rotation
	if angle != 0 {
		img, correction = rotateForOCR(img, angle)
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		return textselect.Layout{}, err
	}
	if err := ctx.Err(); err != nil {
		return textselect.Layout{}, err
	}
	cmd := t.command(ctx, "stdin", "stdout", "-l", "ita+eng", "--oem", "1", "--psm", "3", "-c", "tessedit_create_tsv=1", "-c", "thresholding_method=2")
	cmd.Stdin = &input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if ctx.Err() != nil {
		return textselect.Layout{}, ctx.Err()
	}
	if err != nil {
		return textselect.Layout{}, fmt.Errorf("Tesseract failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	layout, err := ParseTSV(bytes.NewReader(data), img.Bounds().Dx(), img.Bounds().Dy())
	if err == nil && angle != 0 {
		layout = correction.restore(layout)
	}
	return layout, err
}

func ParseTSV(r io.Reader, width, height int) (textselect.Layout, error) {
	l := textselect.Layout{Width: float64(width), Height: float64(height)}
	if width <= 0 || height <= 0 {
		return l, errors.New("invalid OCR image dimensions")
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() || scanner.Text() != "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext" {
		return l, errors.New("invalid Tesseract TSV header")
	}
	blocks, lines := map[string]int{}, map[string]int{}
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 12)
		if len(fields) != 12 {
			return l, errors.New("invalid Tesseract TSV row")
		}
		if fields[0] != "5" || strings.TrimSpace(fields[11]) == "" {
			continue
		}
		var n [4]int
		for i := range n {
			v, e := strconv.Atoi(fields[6+i])
			if e != nil {
				return l, e
			}
			n[i] = v
		}
		confidence, e := strconv.ParseFloat(fields[10], 64)
		if e != nil {
			return l, e
		}
		x, y, w, h := n[0], n[1], n[2], n[3]
		if x < 0 || y < 0 || w <= 0 || h <= 0 || x > width || y > height || w > width-x || h > height-y {
			return l, errors.New("OCR word outside image bounds")
		}
		bk := strings.Join(fields[1:3], "/")
		lk := strings.Join(fields[1:5], "/")
		if _, ok := blocks[bk]; !ok {
			blocks[bk] = len(blocks)
		}
		if _, ok := lines[lk]; !ok {
			lines[lk] = len(lines)
		}
		l.Words = append(l.Words, textselect.Word{Text: fields[11], Quad: textselect.Rect(float64(x)/l.Width, float64(y)/l.Height, float64(x+w)/l.Width, float64(y+h)/l.Height), Block: blocks[bk], Line: lines[lk], Confidence: confidence})
	}
	return l, scanner.Err()
}
