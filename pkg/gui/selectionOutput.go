package gui

import (
	"bytes"
	"context"
	"fmt"
	"image"

	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/internal/printing"
	"emanuelelongo/proview/pkg/document"
)

type selectedPageSource struct {
	snapshot *document.Document
	markdown *markdown.Document
	pages    []int
}

// Capture all editor state on the UI thread; rendering uses only these immutable
// sources. Never substitute sidebar thumbnails for full-resolution content.
func (g *Gui) selectedPageSources() ([]selectedPageSource, error) {
	if g.document != nil && g.document.Format() == document.FormatPDF {
		return []selectedPageSource{{snapshot: g.document.RenderingSnapshot(), pages: append([]int(nil), g.selectedPDFPages()...)}}, nil
	}
	entries := g.selectedDocuments()
	if len(entries) == 0 {
		if g.document != nil {
			return []selectedPageSource{{snapshot: g.document.RenderingSnapshot()}}, nil
		}
		if g.showingMarkdown() {
			return []selectedPageSource{{markdown: g.markdownView.doc}}, nil
		}
	}
	sources := make([]selectedPageSource, 0, len(entries))
	for _, entry := range entries {
		doc, md := entry.doc, entry.markdown
		if entry == g.activeDocument {
			doc = g.document
			if g.showingMarkdown() {
				md = g.markdownView.doc
			}
		}
		switch {
		case doc != nil:
			sources = append(sources, selectedPageSource{snapshot: doc.RenderingSnapshot()})
		case md != nil:
			sources = append(sources, selectedPageSource{markdown: md})
		default:
			return nil, fmt.Errorf("select images, PDF pages or Markdown documents; crop a model view to use it as an image")
		}
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no items selected")
	}
	return sources, nil
}

func prepareSelectedOutput(ctx context.Context, sources []selectedPageSource, layout *document.PDFPageLayout) (printing.Job, error) {
	if len(sources) == 0 {
		return printing.Job{}, fmt.Errorf("no items selected")
	}
	if layout != nil {
		if err := layout.Validate(); err != nil {
			return printing.Job{}, err
		}
	}
	var jobs []printing.Job
	var parts [][]byte
	combined := printing.Job{Title: "Selected items"}
	encodedBytes := 0
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return printing.Job{}, err
		}
		var job printing.Job
		var err error
		if source.markdown != nil {
			job, err = prepareMarkdownPrintJob(ctx, source.markdown)
		} else {
			job, err = preparePrintJob(ctx, source.snapshot, source.pages)
		}
		if err != nil {
			return printing.Job{}, err
		}
		encodedBytes += len(job.PDF)
		if len(sources) > 1 && encodedBytes > 256<<20 {
			return printing.Job{}, fmt.Errorf("selected documents exceed the 256 MiB combined PDF limit")
		}
		jobs = append(jobs, job)
		parts = append(parts, job.PDF)
		combined.Pages = append(combined.Pages, job.Pages...)
	}
	if len(jobs) == 1 {
		combined = jobs[0]
	} else {
		data, err := document.CombinePrintPDFs(ctx, parts)
		if err != nil {
			return printing.Job{}, err
		}
		if len(data) > 256<<20 {
			return printing.Job{}, fmt.Errorf("combined PDF exceeds 256 MiB")
		}
		combined.PDF = data
		combined.RenderPage = func(ctx context.Context, page int, dpi float64) (image.Image, error) {
			if page < 0 {
				return nil, document.ErrInvalidRect
			}
			for _, job := range jobs {
				if page < len(job.Pages) {
					return job.RenderPage(ctx, page, dpi)
				}
				page -= len(job.Pages)
			}
			return nil, document.ErrInvalidRect
		}
	}
	if layout != nil {
		data, err := document.NormalizePrintPDF(ctx, combined.PDF, *layout)
		if err != nil {
			return printing.Job{}, err
		}
		combined.PDF = data
		width, height := layout.Points()
		for i := range combined.Pages {
			combined.Pages[i] = printing.PageSize{Width: width, Height: height}
		}
		combined.RenderPage = func(ctx context.Context, page int, dpi float64) (image.Image, error) {
			return renderPreparedPrintPage(ctx, data, page, dpi)
		}
	}
	return combined, nil
}

func (g *Gui) requestCreatePDF() {
	g.activateEditor()
	if g.waitAnnotationChanges(g.requestCreatePDF) || g.closed || g.working || !g.selectionCanOutput() {
		return
	}
	g.window.Canvas().Focus(nil)
	sources, err := g.selectedPageSources()
	if err != nil {
		g.showOperationError("create_pdf", err)
		return
	}
	g.choosePDFLayout(sources)
}

func (g *Gui) createSelectionPDF(sources []selectedPageSource, layout *document.PDFPageLayout) {
	g.runFileWork("Creating PDF", func() (*document.Document, error) {
		job, err := prepareSelectedOutput(context.Background(), sources, layout)
		if err != nil {
			return nil, err
		}
		source, err := document.LoadFrom("Selection.pdf", "", bytes.NewReader(job.PDF))
		if err != nil {
			return nil, err
		}
		doc, err := document.New(source)
		if err != nil {
			source.Close()
			return nil, err
		}
		doc.MarkUnsaved()
		if _, err := doc.Preview(0); err != nil {
			doc.Close()
			return nil, err
		}
		return doc, nil
	}, func(doc *document.Document) {
		if g.closed {
			doc.Close()
			return
		}
		// A PDF owns its own window, including when extracting pages from a PDF.
		editor := g.newEditor()
		editor.addDocument(doc, nil, nil)
		editor.showWindow()
	})
}
