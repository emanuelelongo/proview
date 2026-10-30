package gui

import (
	"context"
	"emanuelelongo/proview/internal/markdown"
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/pkg/document"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	xdraw "golang.org/x/image/draw"
)

// Each retained document owns its history, save destination and view state.
type editorDocument struct {
	thumbnail       image.Image
	thumbnailSource image.Image
	thumbnailCancel context.CancelFunc
	thumbnailFailed bool
	markdown        *markdown.Document
	markdownScale   float32
	markdownSource  bool
	markdownDetails map[*markdown.Details]bool
	markdownView    *markdownView
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

const maxImageThumbnailDimension = 320

func downsampleImageThumbnail(src image.Image) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxImageThumbnailDimension && h <= maxImageThumbnailDimension {
		return src
	}
	var nw, nh int
	if w >= h {
		nw = maxImageThumbnailDimension
		nh = max(1, int(math.Round(float64(h)*float64(maxImageThumbnailDimension)/float64(w))))
	} else {
		nh = maxImageThumbnailDimension
		nw = max(1, int(math.Round(float64(w)*float64(maxImageThumbnailDimension)/float64(h))))
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)
	return dst
}

// thumbnailFor reuses a sidebar thumbnail while the source image identity is
// unchanged. Edited documents replace their preview image instead of mutating it.
func (e *editorDocument) thumbnailFor(src image.Image) image.Image {
	if e.thumbnail != nil && e.thumbnailSource == src {
		return e.thumbnail
	}
	e.thumbnailSource = src
	e.thumbnail = downsampleImageThumbnail(src)
	return e.thumbnail
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
		entry.thumbnailFor(g.image.Image)
	}
	entry.doc = g.document
	if !g.showingMarkdown() && !g.showingModel() {
		entry.target = g.saveTarget
	}
	entry.redactedTargets = g.redactedTargets
	entry.zoom, entry.zoomLevel, entry.page = g.zoom, g.zoomLevel, g.activePDFPage
	entry.offset = g.imageScroll.Offset
	if g.document != nil && g.document.Format() == document.FormatPDF {
		entry.offset = g.pdfScroll.Offset
	}
	previousMarkdown := entry.markdown
	entry.markdown = nil
	if g.showingMarkdown() {
		entry.markdown = g.markdownView.doc
		entry.markdownView = g.markdownView
		entry.markdownScale = g.markdownView.scale
		entry.markdownSource = g.markdownView.sourceMode
		entry.markdownDetails = g.markdownView.detailsOpen
		entry.offset = g.markdownView.scroll.Offset
		if g.markdownView.pendingReadingOffset != nil {
			entry.offset = *g.markdownView.pendingReadingOffset
		}
	}
	if previousMarkdown != nil && previousMarkdown != entry.markdown {
		previousMarkdown.Close()
	}
	entry.mesh = nil
	if g.showingModel() {
		entry.mesh, entry.camera = g.modelViewport.mesh, g.modelViewport.camera
	}
}

func (g *Gui) prepareDocumentSwitch() {
	g.stopGIFPlayback()
	g.openingFitDocument = nil
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
	if g.activeDocument != nil && g.activeDocument.doc != nil {
		g.activeDocument.doc.TrimInactive()
	}
	if g.activeDocument != nil && g.activeDocument.markdown != nil {
		md := g.activeDocument.markdown
		if md.HasDecodedImages() {
			if g.activeDocument.markdownView != nil {
				g.activeDocument.markdownView.suspend()
			}
			md.TrimImages()
		}
	}
}

func (g *Gui) addDocument(doc *document.Document, mesh *model3d.Mesh, target fyne.URI) {
	g.activateEditor()
	g.prepareDocumentSwitch()
	entry := &editorDocument{doc: doc, mesh: mesh, target: target}
	g.documents = append(g.documents, entry)
	g.activeDocument = entry
	g.singleDocumentSelection(entry)
	// Ownership stays in documents while the old view is detached.
	g.document = nil
	if mesh != nil {
		g.showModel(mesh)
	} else {
		g.showDocument(doc)
		g.saveTarget = target
	}
	g.refreshDocumentSidebar()
	if target != nil {
		g.sizeWindowForOpenedFile()
	}
	g.captureDocument()
}

func (g *Gui) selectDocument(entry *editorDocument) {
	g.selectDocumentThumbnail(entry, 0)
}

func (g *Gui) activateDocument(entry *editorDocument) {
	if entry.markdown != nil && entry.markdown.ImagesTrimmed() {
		g.working = true
		g.clearMarkdown()
		g.markdownPanel.Add(widget.NewLabel("Restoring Markdown images…"))
		g.markdownPanel.Show()
		doc := entry.markdown
		go func() {
			doc.RestoreImages()
			fyne.Do(func() {
				if g.closed {
					return
				}
				g.working = false
				if !slices.Contains(g.documents, entry) || entry.markdown != doc {
					g.markdownPanel.RemoveAll()
					g.markdownPanel.Hide()
					return
				}
				g.markdownPanel.RemoveAll()
				g.activateDocumentReady(entry)
			})
		}()
		return
	}
	g.activateDocumentReady(entry)
}

func (g *Gui) activateDocumentReady(entry *editorDocument) {
	g.activeDocument = entry
	g.document = nil
	if entry.markdown != nil {
		g.showMarkdown(entry.markdown)
		if entry.markdownScale > 0 && entry.markdownScale != g.markdownView.scale {
			g.markdownView.scale = entry.markdownScale
			g.markdownView.rebuild()
		}
		g.markdownView.restoreReadingOffset(entry.offset)
	} else if entry.mesh != nil {
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
	g.cancelDocumentDrag()
	g.documentThumbs = nil
	g.normalizeDocumentSelection()
	if g.activeDocument != nil && g.document != nil && g.document.Format() != document.FormatPDF {
		g.activeDocument.thumbnailFor(g.image.Image)
	}
	for _, entry := range g.documents {
		g.ensureMarkdownThumbnail(entry)
		g.ensureModelThumbnail(entry)
	}
	// Reuse immutable image/model previews; switching never renders on the UI thread.
	g.documentList.RemoveAll()
	if len(g.documents) > 1 {
		for index, entry := range g.documents {
			if index > 0 {
				divider := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
				divider.SetMinSize(fyne.NewSize(0, 2))
				g.documentList.Add(divider)
			}
			if entry.thumbnail != nil && (entry.markdown != nil || entry.mesh != nil || entry.doc != nil && entry.doc.Format() != document.FormatPDF) {
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
				if !g.documentSelection[entry] {
					border.Hide()
				}
				tap := newThumbnailTap(nil)
				tap.onSelect = func(modifiers fyne.KeyModifier) { g.selectDocumentThumbnail(entry, modifiers) }
				tap.onSecondary = func(event *fyne.PointEvent) { g.documentContextMenu(entry, event) }
				card := container.NewStack(preview, border, tap)
				g.configureDocumentDrag(entry, card, tap, preview)
				g.documentList.Add(card)
				continue
			}
			name := ""
			if entry.doc != nil {
				name = entry.doc.Name()
				if entry.doc.Dirty() {
					name = "• " + name
				}
			} else if entry.markdown != nil {
				name = entry.markdown.Name
			} else if entry.mesh != nil {
				name = "Loading…"
				if entry.thumbnailFailed {
					name = "Preview unavailable"
				}
			}
			button := widget.NewButton(name, func() { g.selectDocumentThumbnail(entry, g.modifiers()) })
			button.Alignment = widget.ButtonAlignLeading
			if g.documentSelection[entry] {
				button.Importance = widget.HighImportance
			}
			tap := newThumbnailTap(nil)
			tap.onSelect = func(modifiers fyne.KeyModifier) { g.selectDocumentThumbnail(entry, modifiers) }
			tap.onSecondary = func(event *fyne.PointEvent) { g.documentContextMenu(entry, event) }
			card := container.NewStack(button, tap)
			g.configureDocumentDrag(entry, card, tap, nil)
			g.documentList.Add(card)
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
		g.sidebar.panel.Objects = []fyne.CanvasObject{g.sidebar.documents, g.sidebar.viewport}
	} else {
		g.sidebar.panel.Layout = layout.NewStackLayout()
		g.sidebar.panel.Objects = []fyne.CanvasObject{g.sidebar.documents}
	}
	g.sidebar.panel.Refresh()
	if g.sidebarSplit != nil {
		g.sidebarSplit.relayout()
	}
	g.updateSelectionActions()
}

func (g *Gui) documentContextMenu(entry *editorDocument, event *fyne.PointEvent) {
	if g.closed || g.working || !slices.Contains(g.documents, entry) {
		return
	}
	if !g.documentSelection[entry] {
		g.singleDocumentSelection(entry)
		g.refreshDocumentSidebar()
	}
	entries := g.selectedDocuments()
	run := func(action func()) {
		if slices.Equal(g.selectedDocuments(), entries) {
			action()
		}
	}
	printLabel := "Print…"
	createLabel := "Create PDF"
	if len(entries) > 1 {
		printLabel = "Print selected items…"
		createLabel = "Create PDF from selected items"
	}
	printItems := fyne.NewMenuItem(printLabel, func() { run(g.requestPrintSelection) })
	printItems.Disabled = !g.selectionCanOutput()
	createPDF := fyne.NewMenuItem(createLabel, func() { run(g.requestCreatePDF) })
	createPDF.Disabled = !g.selectionCanOutput()
	closeFile := fyne.NewMenuItem("Close file", func() { g.closeDocumentEntry(entry) })
	closeFile.Icon = tablerIcon("x")
	menu := widget.NewPopUpMenu(fyne.NewMenu("", printItems, createPDF, fyne.NewMenuItemSeparator(), closeFile), g.window.Canvas())
	menu.ShowAtPosition(event.AbsolutePosition)
}

func (g *Gui) receiveDocument(doc *document.Document, mesh *model3d.Mesh, target fyne.URI, done func()) {
	incomingPDF := doc != nil && doc.Format() == document.FormatPDF
	currentPDF := g.document != nil && g.document.Format() == document.FormatPDF
	if g.hasOpenContent() && incomingPDF != currentPDF {
		editor := g.newEditor()
		editor.addDocument(doc, mesh, target)
		editor.showWindow()
		done()
		return
	}
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
			editor.showWindow()
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
	delete(g.documentSelection, g.activeDocument)
	for i, entry := range g.documents {
		if entry == g.activeDocument {
			entry.markdownView = nil
			if entry.markdown != nil {
				entry.markdown.Close()
				entry.markdown = nil
			}
			entry.thumbnail, entry.thumbnailSource = nil, nil
			if entry.thumbnailCancel != nil {
				entry.thumbnailCancel()
			}
			g.documents = slices.Delete(g.documents, i, i+1)
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
		entry.markdownView = nil
		if entry.markdown != nil {
			entry.markdown.Close()
			entry.markdown = nil
		}
		entry.thumbnail, entry.thumbnailSource = nil, nil
		if entry.thumbnailCancel != nil {
			entry.thumbnailCancel()
		}
		if entry.doc != nil && entry.doc != g.document {
			doc := entry.doc
			go func() {
				doc.Close()
				requestMemoryRelease()
			}()
		}
	}
	g.documents = nil
	g.activeDocument = nil
	g.singleDocumentSelection(nil)
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
	editor.workDir = g.workDir
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
			g.editors.windows = slices.Delete(g.editors.windows, index, index+1)
			break
		}
	}
	if g.editors.active == g {
		g.editors.active = nil
	}
}
