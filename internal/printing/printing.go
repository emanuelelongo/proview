// Package printing submits prepared PDFs through the desktop print dialog.
package printing

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

var ErrCanceled = errors.New("printing canceled")

type PageSize struct{ Width, Height float64 } // PDF points

// Job owns an immutable print snapshot. RenderPage is used only for the Windows
// printer DC, never as a PDF export fallback. Other backends submit the PDF.
type Job struct {
	Title        string
	PDF          []byte
	Pages        []PageSize
	ParentWindow uintptr
	ParentID     string
	RenderPage   func(context.Context, int, float64) (image.Image, error)
}

func Print(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(job.PDF) == 0 || len(job.Pages) == 0 {
		return fmt.Errorf("printing: empty document")
	}
	for _, size := range job.Pages {
		if size.Width <= 0 || size.Height <= 0 {
			return fmt.Errorf("printing: invalid page dimensions")
		}
	}
	return withPrintFile(job.PDF, func(path string) error { return printNative(ctx, job, path) })
}

// Retain the private PDF until the native operation has consumed it. In
// particular, launching an external viewer and immediately deleting it is unsafe.
func withPrintFile(data []byte, consume func(string) error) error {
	dir, err := os.MkdirTemp("", "proview-print-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "document.pdf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return consume(path)
}
