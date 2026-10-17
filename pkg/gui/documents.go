package gui

import (
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Each retained document owns its history, save destination and view state.
type editorDocument struct {
	thumbnail       image.Image
	doc             *document.Document
	mesh            *model3d.Mesh
	camera          model3d.Camera
	target          fyne.URI
	redactedTargets map[string][]string
	zoom            float32
	zoomLevel, page int
	offset          fyne.Position
}

func (g *Gui) retainsDocument(doc *document.Document) bool {
	for _, entry := range g.documents {
		if entry.doc == doc {
			return true
		}
	}
	return false
}

func (g *Gui) captureDocument() {
	if !g.hasOpenContent() {
		return
	}
	entry := g.activeDocument
	if entry == nil {
		entry = &editorDocument{}
		g.documents = append(g.documents, entry)
		g.activeDocument = entry
	}
	if g.document != nil && g.document.Format() != document.FormatPDF {
		entry.thumbnail = g.image.Image
	}
	entry.doc, entry.target = g.document, g.saveTarget
	entry.redactedTargets = g.redactedTargets
	entry.zoom, entry.zoomLevel, entry.page = g.zoom, g.zoomLevel, g.activePDFPage
	entry.offset = g.imageScroll.Offset
	if g.document != nil && g.document.Format() == document.FormatPDF {
		entry.offset = g.pdfScroll.Offset
	}
	entry.mesh = nil
	if g.showingModel() {
		entry.mesh, entry.camera = g.modelViewport.mesh, g.modelViewport.camera
	}
}

func (g *Gui) prepareDocumentSwitch() {
	g.window.Canvas().Focus(nil)
	g.setTextEditing(false)
	g.setFormsActive(false)
	g.closeSearch()
	g.stopOCR()
	g.clearAreaSelection()
	g.clearRedactionSelection()
	g.setAnnotationTool(toolSelect)
	g.panModifierDown = false
	g.setPanActive(false)
	g.captureDocument()
}

func (g *Gui) addDocument(doc *document.Document, mesh *model3d.Mesh, target fyne.URI) {
	g.activateEditor()
	g.prepareDocumentSwitch()
	entry := &editorDocument{doc: doc, mesh: mesh, target: target}
	g.documents = append(g.documents, entry)
	g.activeDocument = entry
	// Ownership stays in documents while the old view is detached.
	g.document = nil
	if mesh != nil {
		g.showModel(mesh)
	} else {
		g.showDocument(doc)
		g.saveTarget = target
	}
	g.captureDocument()
	g.refreshDocumentSidebar()
}

func (g *Gui) selectDocument(entry *editorDocument) {
	g.activateEditor()
	if entry == g.activeDocument || g.closed {
		return
	}
	if g.waitAnnotationChanges(func() { g.selectDocument(entry) }) || g.working || g.pdfOpenDialog != nil {
		return
	}
	g.logEvent("document_switch", "next_document=%p model=%t", entry.doc, entry.mesh != nil)
	g.prepareDocumentSwitch()
	g.activateDocument(entry)
}

func (g *Gui) activateDocument(entry *editorDocument) {
	g.activeDocument = entry
	g.document = nil
	if entry.mesh != nil {
		g.showModel(entry.mesh)
		g.modelViewport.camera = entry.camera
		g.modelViewport.requestRender()
	} else {
		g.showDocument(entry.doc)
		g.saveTarget, g.redactedTargets = entry.target, entry.redactedTargets
		if entry.zoom > 0 {
			g.zoom, g.zoomLevel = entry.zoom, entry.zoomLevel
			g.applyZoom()
		}
		if entry.doc.Format() == document.FormatPDF {
			g.activatePDFPage(min(entry.page, entry.doc.PageCount()-1))
			g.pdfScroll.ScrollToOffset(entry.offset)
			g.loadVisiblePDFPages()
			g.loadVisiblePDFThumbnails()
			g.activatePDFPage(min(entry.page, entry.doc.PageCount()-1))
		} else {
			g.imageScroll.ScrollToOffset(entry.offset)
		}
	}
	g.refreshDocumentSidebar()
}

func (g *Gui) refreshDocumentSidebar() {
	if g.documentList == nil {
		return
	}
	if g.activeDocument != nil && g.document != nil && g.document.Format() != document.FormatPDF {
		g.activeDocument.thumbnail = g.image.Image
	}
	// Reuse the displayed image preview; switching never decodes images on the UI thread.
	g.documentList.RemoveAll()
	if len(g.documents) > 1 {
		for _, entry := range g.documents {
			if entry.doc != nil && entry.doc.Format() != document.FormatPDF && entry.thumbnail != nil {
				preview := canvas.NewImageFromImage(entry.thumbnail)
				preview.FillMode = canvas.ImageFillContain
				preview.ScaleMode = canvas.ImageScaleFastest
				width := defaultSidebarWidth
				if g.sidebarSplit != nil {
					width = g.sidebarSplit.width
				}
				preview.SetMinSize(thumbnailSizeForSidebar(entry.thumbnail.Bounds().Size(), width))
				border := canvas.NewRectangle(color.Transparent)
				border.StrokeColor = theme.PrimaryColor()
				border.StrokeWidth = 3
				if entry != g.activeDocument {
					border.Hide()
				}
				card := container.NewStack(preview, border, newThumbnailTap(func() { g.selectDocument(entry) }))
				if entry.doc.Dirty() {
					g.documentList.Add(container.NewVBox(card, widget.NewLabel("•")))
				} else {
					g.documentList.Add(card)
				}
				continue
			}
			name := ""
			if entry.doc != nil {
				name = entry.doc.Name()
				if entry.doc.Dirty() {
					name = "• " + name
				}
			} else if entry.mesh != nil {
				name = entry.mesh.Name
			}
			button := widget.NewButton(name, func() { g.selectDocument(entry) })
			button.Alignment = widget.ButtonAlignLeading
			if entry == g.activeDocument {
				button.Importance = widget.HighImportance
			}
			g.documentList.Add(button)
		}
	}
	if g.document != nil && g.document.Format() == document.FormatPDF {
		g.thumbList.Show()
	} else {
		g.thumbList.Hide()
	}
	if len(g.documents) > 1 {
		g.sidebar.documents.Show()
	} else {
		g.sidebar.documents.Hide()
	}
	if g.document != nil && g.document.Format() == document.FormatPDF {
		g.sidebar.container.Show()
	} else {
		g.sidebar.container.Hide()
	}
	if len(g.documents) > 1 || (g.document != nil && g.document.Format() == document.FormatPDF) {
		g.sidebar.panel.Show()
	} else {
		g.sidebar.panel.Hide()
	}
	if g.document != nil && g.document.Format() == document.FormatPDF {
		g.sidebar.panel.Layout = layout.NewBorderLayout(g.sidebar.documents, nil, nil, nil)
		g.sidebar.panel.Objects = []fyne.CanvasObject{g.sidebar.documents, g.sidebar.container}
	} else {
		g.sidebar.panel.Layout = layout.NewStackLayout()
		g.sidebar.panel.Objects = []fyne.CanvasObject{g.sidebar.documents}
	}
	g.sidebar.panel.Refresh()
	if g.sidebarSplit != nil {
		g.sidebarSplit.Refresh()
	}
}

func (g *Gui) receiveDocument(doc *document.Document, mesh *model3d.Mesh, target fyne.URI, done func()) {
	if doc == nil || doc.Format() != document.FormatPDF || g.document == nil || g.document.Format() != document.FormatPDF {
		g.addDocument(doc, mesh, target)
		done()
		return
	}
	current := g.document
	prompt := dialog.NewCustomWithoutButtons("Open PDF", widget.NewLabel(fmt.Sprintf("How would you like to open %s?", doc.Name())), g.window)
	g.pdfOpenDialog = prompt
	g.logEvent("pdf_open_choice", "incoming_pages=%d", doc.PageCount())
	consumed := false
	prompt.SetOnClosed(func() {
		g.pdfOpenDialog = nil
		if !consumed {
			g.logEvent("pdf_open_decision", "choice=cancel")
			doc.Close()
		}
		done()
	})
	prompt.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Cancel", prompt.Hide),
		widget.NewButton("New window", func() {
			g.logEvent("pdf_open_decision", "choice=new_window")
			consumed = true
			editor := g.newEditor()
			editor.addDocument(doc, nil, target)
			editor.window.Show()
			prompt.Hide()
		}),
		widget.NewButton("Append pages", func() {
			if g.closed || g.document != current || g.working {
				return
			}
			if g.waitAnnotationChanges(func() {}) {
				return
			}
			g.logEvent("pdf_open_decision", "choice=append incoming_pages=%d", doc.PageCount())
			first := current.PageCount()
			if err := current.AppendDocumentPages(doc); err != nil {
				g.showOperationError("pdf_append", err)
				return
			}
			consumed = true // Sources now belong to current, including its Undo history.
			g.rebuildPDFWithCompletion(first, func() { g.revealThumbnail(first) })
			prompt.Hide()
		}),
	})
	prompt.Show()
}

func (g *Gui) processPendingFiles() {
	if g.closed || g.working || g.openingFiles || g.externalOpenPauses > 0 || g.pdfOpenDialog != nil || len(g.pendingFiles) == 0 {
		return
	}
	if g.waitAnnotationChanges(g.processPendingFiles) {
		return
	}
	path := g.pendingFiles[0]
	g.pendingFiles = g.pendingFiles[1:]
	g.openingFiles = true
	reader, err := startupFileReader(path)
	g.openFile(reader, err, func() { g.openingFiles = false; g.processPendingFiles() })
}

func (g *Gui) removeActiveDocument() {
	for i, entry := range g.documents {
		if entry == g.activeDocument {
			g.documents = append(g.documents[:i], g.documents[i+1:]...)
			break
		}
	}
	g.activeDocument = nil
}
func (g *Gui) restoreRemainingDocument() {
	if len(g.documents) > 0 {
		g.activateDocument(g.documents[len(g.documents)-1])
	} else {
		g.refreshDocumentSidebar()
	}
}
func (g *Gui) closeRetainedDocuments() {
	for _, entry := range g.documents {
		if entry.doc != nil && entry.doc != g.document {
			go entry.doc.Close()
		}
	}
	g.documents = nil
	g.activeDocument = nil
}
func (g *Gui) confirmWindowDocuments(done func()) {
	g.captureDocument()
	entries := append([]*editorDocument(nil), g.documents...)
	var confirm func(int)
	confirm = func(index int) {
		if index >= len(entries) {
			done()
			return
		}
		entry := entries[index]
		if entry.doc == nil || !entry.doc.Dirty() {
			confirm(index + 1)
			return
		}
		if entry != g.activeDocument {
			g.selectDocument(entry)
		}
		g.confirmUnsavedChanges("Save changes before closing?", func() { confirm(index + 1) })
	}
	confirm(0)
}

// Modal save/close decisions must keep their original document active.
func (g *Gui) pauseExternalOpening(prompt *dialog.CustomDialog) {
	g.externalOpenPauses++
	prompt.SetOnClosed(func() {
		g.externalOpenPauses--
		// Finish the button's action before accepting another document.
		fyne.Do(g.processPendingFiles)
	})
}

// Desktop requests follow the last editor used by the shared application.
type editorGroup struct {
	windows []*Gui
	active  *Gui
}

func (g *Gui) activateEditor() {
	if g.editors != nil && !g.closed {
		g.editors.active = g
	}
}
func (g *Gui) newEditor() *Gui {
	editor := newGui(g.app)
	if g.editors == nil {
		g.editors = &editorGroup{windows: []*Gui{g}}
	}
	editor.editors = g.editors
	g.editors.windows = append(g.editors.windows, editor)
	editor.activateEditor()
	return editor
}
func (g *Gui) desktopEditor() *Gui {
	if g.editors != nil {
		if editor := g.editors.active; editor != nil && !editor.closed {
			return editor
		}
		for _, editor := range g.editors.windows {
			if !editor.closed {
				return editor
			}
		}
	}
	return g
}

func (g *Gui) unregisterEditor() {
	if g.editors == nil {
		return
	}
	for index, editor := range g.editors.windows {
		if editor == g {
			g.editors.windows = append(g.editors.windows[:index], g.editors.windows[index+1:]...)
			break
		}
	}
	if g.editors.active == g {
		g.editors.active = nil
	}
}
