package gui

import (
	"strings"
	"testing"

	"emanuelelongo/proview/internal/markdown"
)

func TestSwitchingToMarkdownReleasesPDFView(t *testing.T) {
	g, driver := newDropTestGui(t)
	defer g.closeWindow()
	md, err := markdown.Load("notes.md", "", strings.NewReader("# Notes"))
	if err != nil {
		t.Fatal(err)
	}
	g.addMarkdown(md, nil)
	notes := g.activeDocument
	g.addDocument(workspacePDF(t, 3), nil, nil)
	if len(g.pdfPageImages) == 0 || len(g.signatureLayers) == 0 {
		t.Fatal("PDF view was not built")
	}
	g.selectDocument(notes)
	driver.drain()
	if g.activeDocument != notes || g.markdownView == nil {
		t.Fatal("Markdown document was not activated")
	}
	if g.pdfPageImages != nil || g.pdfThumbImages != nil || len(g.signatureLayers) != 0 || len(g.pdfPages.Objects) != 0 || len(g.thumbList.Objects) != 0 || g.previewRenderer != nil || g.image.Image != nil {
		t.Fatal("inactive PDF view stayed referenced by the Markdown view")
	}
}
