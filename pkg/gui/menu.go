package gui

import (
	"bytes"
	"emanuelelongo/proview/internal/model3d"
	"emanuelelongo/proview/pkg/document"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func BuildMenu(g *Gui) {
	shortcutModifier := fyne.KeyModifierShortcutDefault
	toolModifier := shortcutModifier | fyne.KeyModifierShift
	openItem := fyne.NewMenuItem("Open", g.requestOpen)
	openItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: shortcutModifier}
	blankItem := fyne.NewMenuItem("New page", g.newBlankPage)
	blankItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: shortcutModifier}
	clipboardItem := fyne.NewMenuItem("New from clipboard", g.newFromClipboard)
	clipboardItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyN, Modifier: toolModifier}
	openTogetherItem := fyne.NewMenuItem("Open PDFs Together", func() { g.openPDFCollection(false) })
	appendItem := fyne.NewMenuItem("Append PDFs", func() { g.openPDFCollection(true) })
	deletePageItem := fyne.NewMenuItem("Delete Page", func() { g.deletePDFPage(g.activePDFPage) })
	saveItem := fyne.NewMenuItem("Save", g.save)
	saveItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: shortcutModifier}
	saveAsItem := fyne.NewMenuItem("Save As", g.saveAs)
	saveAsItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: shortcutModifier | fyne.KeyModifierShift}
	closeItem := fyne.NewMenuItem("Close", g.requestCloseDocument)
	closeItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: shortcutModifier}
	exitItem := fyne.NewMenuItem("Exit", g.requestClose)
	exitItem.IsQuit = true
	fileMenu := fyne.NewMenu("File",
		openItem, blankItem, clipboardItem, openTogetherItem, appendItem, deletePageItem,
		saveItem, saveAsItem, closeItem, fyne.NewMenuItemSeparator(),
		exitItem,
	)

	undoItem := fyne.NewMenuItem("Undo", g.undo)
	undoItem.Shortcut = &fyne.ShortcutUndo{}
	redoItem := fyne.NewMenuItem("Redo", g.redo)
	redoItem.Shortcut = &fyne.ShortcutRedo{}
	copyItem := fyne.NewMenuItem("Copy", g.copyArea)
	copyItem.Shortcut = &fyne.ShortcutCopy{}
	pasteItem := fyne.NewMenuItem("Paste", g.pasteImage)
	pasteItem.Shortcut = &fyne.ShortcutPaste{}
	editMenu := fyne.NewMenu("Edit", undoItem, redoItem, fyne.NewMenuItemSeparator(), copyItem, pasteItem)

	zoomInItem := fyne.NewMenuItem("Zoom In", g.zoomIn)
	zoomInItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyPlus, Modifier: shortcutModifier}
	zoomOutItem := fyne.NewMenuItem("Zoom Out", g.zoomOut)
	zoomOutItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: shortcutModifier}
	resetZoomItem := fyne.NewMenuItem("Fit to Window", g.resetZoom)
	resetZoomItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: shortcutModifier}
	viewMenu := fyne.NewMenu("View", zoomInItem, zoomOutItem, resetZoomItem)
	g.modelMenu = viewMenu
	g.modelMenuItems = []*fyne.MenuItem{fyne.NewMenuItemSeparator()}
	for i, name := range modelViews {
		item := fyne.NewMenuItem(name, func() {
			if g.showingModel() {
				g.modelViewport.setView(name)
			}
		})
		item.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyName(fmt.Sprint(i + 1))}
		g.modelMenuItems = append(g.modelMenuItems, item)
	}
	g.modelWireframeItem = fyne.NewMenuItem("Wireframe", g.toggleModelWireframe)
	g.modelWireframeItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.Key0}
	g.modelMenuItems = append(g.modelMenuItems, g.modelWireframeItem)
	prepareModelMenu(g)

	toolItem := func(label string, key fyne.KeyName, action func()) *fyne.MenuItem {
		item := fyne.NewMenuItem(label, action)
		item.Shortcut = &desktop.CustomShortcut{KeyName: key, Modifier: toolModifier}
		return item
	}
	cropItem := toolItem("Crop", fyne.KeyC, g.startCrop)
	selectItem := toolItem("Select", fyne.KeyV, func() { g.setAnnotationTool(toolSelect) })
	drawItem := toolItem("Draw", fyne.KeyD, func() { g.setAnnotationTool(toolDraw) })
	highlightItem := toolItem("Highlight", fyne.KeyH, func() { g.setAnnotationTool(toolHighlight) })
	shapesItem := toolItem("Shapes", fyne.KeyS, func() { g.setAnnotationTool(toolShapes) })
	textItem := toolItem("Text", fyne.KeyT, func() { g.setAnnotationTool(toolText) })
	signatureItem := toolItem("Signature", fyne.KeyI, g.promptSignature)
	redactItem := toolItem("Redact", fyne.KeyR, func() { g.setAnnotationTool(toolRedact) })
	toolsMenu := fyne.NewMenu("Tools", selectItem, cropItem, drawItem, highlightItem, shapesItem, textItem, signatureItem, redactItem)

	showTipsItem := fyne.NewMenuItem("Show Tips", func() { g.setTipsEnabled(!g.tipsEnabled) })
	showTipsItem.Checked = g.tipsEnabled
	g.tipsItem = showTipsItem
	thirdPartyLicensesItem := fyne.NewMenuItem("Third-Party Licenses", func() {
		text := widget.NewLabel(thirdPartyLicensesText())
		text.Selectable = true
		text.Wrapping = fyne.TextWrapWord
		scroll := container.NewScroll(text)
		scroll.SetMinSize(g.signatureViewport())
		dialog.NewCustom("Third-Party Licenses", "Close", scroll, g.window).Show()
	})
	helpMenu := fyne.NewMenu("Help", showTipsItem, fyne.NewMenuItemSeparator(), thirdPartyLicensesItem,
		fyne.NewMenuItemSeparator(), fyne.NewMenuItem("About", g.showAbout))
	g.tipsMenu = helpMenu
	g.window.SetMainMenu(fyne.NewMainMenu(fileMenu, editMenu, viewMenu, toolsMenu, helpMenu))
	g.editMenu = editMenu
	g.saveItem = saveItem
	g.saveAsItem = saveAsItem
	g.signatureItem = signatureItem
	g.undoItem = undoItem
	g.redoItem = redoItem
	g.menuDocumentItems = []*fyne.MenuItem{
		openTogetherItem, appendItem, deletePageItem, saveItem, saveAsItem, closeItem,
		zoomInItem, zoomOutItem, resetZoomItem,
		copyItem, pasteItem, selectItem, cropItem, drawItem, highlightItem, shapesItem, textItem, redactItem,
	}
	g.stateMenus = []*fyne.Menu{fileMenu, editMenu, viewMenu, toolsMenu}

	shortcuts := map[*desktop.CustomShortcut]func(fyne.Shortcut){
		{KeyName: fyne.KeyW, Modifier: shortcutModifier}: func(fyne.Shortcut) { g.requestCloseDocument() },
		{KeyName: fyne.KeyO, Modifier: shortcutModifier}: func(fyne.Shortcut) { g.requestOpen() },
		{KeyName: fyne.KeyN, Modifier: shortcutModifier}: func(fyne.Shortcut) { g.newBlankPage() },
		{KeyName: fyne.KeyN, Modifier: toolModifier}:     func(fyne.Shortcut) { g.newFromClipboard() },
		{KeyName: fyne.KeyC, Modifier: toolModifier}:     func(fyne.Shortcut) { g.startCrop() },
		{KeyName: fyne.KeyV, Modifier: toolModifier}:     func(fyne.Shortcut) { g.setAnnotationTool(toolSelect) },
		{KeyName: fyne.KeyD, Modifier: toolModifier}:     func(fyne.Shortcut) { g.setAnnotationTool(toolDraw) },
		{KeyName: fyne.KeyH, Modifier: toolModifier}:     func(fyne.Shortcut) { g.setAnnotationTool(toolHighlight) },
		{KeyName: fyne.KeyT, Modifier: toolModifier}:     func(fyne.Shortcut) { g.setAnnotationTool(toolText) },
		{KeyName: fyne.KeyI, Modifier: toolModifier}:     func(fyne.Shortcut) { g.promptSignature() },
		{KeyName: fyne.KeyR, Modifier: toolModifier}:     func(fyne.Shortcut) { g.setAnnotationTool(toolRedact) },
	}
	for shortcut, action := range shortcuts {
		g.window.Canvas().AddShortcut(shortcut, action)
	}
	g.window.Canvas().AddShortcut(&fyne.ShortcutCopy{}, func(fyne.Shortcut) { g.copyArea() })
	g.window.Canvas().AddShortcut(&fyne.ShortcutPaste{}, func(fyne.Shortcut) { g.pasteImage() })
	g.window.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyS, Modifier: toolModifier}, func(fyne.Shortcut) { g.setAnnotationTool(toolShapes) })
	g.window.Canvas().AddShortcut(&fyne.ShortcutUndo{}, func(fyne.Shortcut) { g.undo() })
	g.window.Canvas().AddShortcut(&fyne.ShortcutRedo{}, func(fyne.Shortcut) { g.redo() })
	g.window.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyZ,
		Modifier: shortcutModifier | fyne.KeyModifierShift,
	}, func(fyne.Shortcut) { g.redo() })
	for _, shortcut := range zoomInShortcuts() {
		g.window.Canvas().AddShortcut(shortcut, func(fyne.Shortcut) { g.zoomIn() })
	}
	g.window.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: shortcutModifier}, func(fyne.Shortcut) { g.zoomOut() })
	g.window.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.Key0, Modifier: shortcutModifier}, func(fyne.Shortcut) { g.resetZoom() })
	g.updateHistoryActions()
}

func zoomInShortcuts() []*desktop.CustomShortcut {
	modifier := fyne.KeyModifierShortcutDefault
	return []*desktop.CustomShortcut{
		{KeyName: fyne.KeyEqual, Modifier: modifier},
		{KeyName: fyne.KeyEqual, Modifier: modifier | fyne.KeyModifierShift},
		{KeyName: fyne.KeyPlus, Modifier: modifier},
		{KeyName: fyne.KeyPlus, Modifier: modifier | fyne.KeyModifierShift},
		{KeyName: fyne.KeyRightBracket, Modifier: modifier},
		{KeyName: fyne.KeyRightBracket, Modifier: modifier | fyne.KeyModifierShift},
	}
}

func BuildOpenFileCallback(g *Gui) func(fyne.URIReadCloser, error) {
	// Open file handler
	return func(uri fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, g.window)
			return
		}
		if uri == nil {
			return
		}
		if g.working {
			_ = uri.Close()
			return
		}
		target := uri.URI()
		g.working = true
		progress := dialog.NewCustomWithoutButtons("Opening", widget.NewProgressBarInfinite(), g.window)
		progress.Show()
		go func() {
			var loadedDocument *document.Document
			var mesh *model3d.Mesh
			var readErr error
			if isModelFile(target.Name()) {
				mesh, readErr = loadModelReader(uri)
			} else {
				loadedDocument, readErr = loadDocumentReader(uri)
			}
			fyne.Do(func() {
				progress.Hide()
				g.working = false
				if g.closed {
					if loadedDocument != nil {
						loadedDocument.Close()
					}
					return
				}
				if readErr != nil {
					dialog.ShowError(readErr, g.window)
					return
				}
				if mesh != nil {
					g.showModel(mesh)
				} else {
					g.showDocument(loadedDocument)
					g.saveTarget = target
				}
			})
		}()
	}
}

func loadDocumentReader(uri fyne.URIReadCloser) (*document.Document, error) {
	name, location := uri.URI().Name(), uri.URI().String()
	data, err := io.ReadAll(uri)
	err = errors.Join(err, uri.Close())
	if err != nil {
		return nil, err
	}
	source, err := document.LoadFrom(name, location, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	loadedDocument, err := document.New(source)
	if err != nil {
		source.Close()
		return nil, err
	}
	if loadedDocument.Format() == document.FormatPDF {
		if _, err := loadedDocument.Preview(0); err != nil {
			loadedDocument.Close()
			return nil, err
		}
	}
	return loadedDocument, nil
}

func (g *Gui) showDocument(loadedDocument *document.Document) {
	g.clearModel()
	g.clearAreaSelection()
	g.cancelThumbnailDrag()
	g.pdfThumbDropIndicators = nil
	g.pdfGeneration++
	g.deactivatePDFCrops()
	g.pdfCrops = nil
	g.pdfPageIDs = nil
	if loadedDocument.Format() == document.FormatPDF {
		g.showPDF(loadedDocument)
		return
	}
	preview, err := loadedDocument.Preview(0)
	if err != nil {
		dialog.ShowError(err, g.window)
		return
	}

	g.clearSignatureSelection()
	g.activePDFPage = 0
	g.signatureLayers = map[int]*signatureLayer{0: g.imageSignatureLayer}
	g.panOverlays = []*panOverlay{g.imagePanOverlay}
	g.imageSignatureLayer.SetDocument(loadedDocument, 0, preview.Bounds().Size())
	g.imageSignatureLayer.SetZoom(1)
	g.imageSignatureLayer.Show()
	g.pdfThumbImages = nil
	g.pdfThumbBorders = nil
	g.pdfThumbLoaded = nil
	g.pdfThumbLoading = nil
	g.pdfPageImages = nil
	g.pdfPageBorders = nil
	g.pdfPageSizes = nil
	g.pdfPageLoaded = nil
	g.pdfPageLoading = nil
	g.pdfPageContainers = nil
	g.cropOverlay.Deactivate()
	g.pdfPages.Objects = nil
	g.pdfPages.Refresh()
	g.thumbList.Objects = nil
	g.thumbList.Refresh()
	g.pdfScroll.Hide()
	g.sidebar.container.Hide()
	g.image.Show()
	g.imageScroll.Show()
	g.image.Image = preview
	g.image.SetMinSize(fyne.NewSize(float32(g.image.Image.Bounds().Dx()), float32(g.image.Image.Bounds().Dy())))
	g.image.Refresh()
	g.setDocument(loadedDocument)
	g.resetZoom()
	g.updateTitle()
	g.updateHistoryActions()
}

func (g *Gui) showPDF(loadedDocument *document.Document) {
	g.clearAreaSelection()
	g.cancelThumbnailDrag()
	g.pdfGeneration++
	generation := g.pdfGeneration
	g.deactivatePDFCrops()
	g.pdfCrops = make(map[int]*cropOverlay)
	g.pdfPageIDs = loadedDocument.PageIDs()
	objects := make([]fyne.CanvasObject, 0, loadedDocument.PageCount())
	pageContainers := make([]fyne.CanvasObject, 0, loadedDocument.PageCount())
	thumbs := make([]fyne.CanvasObject, 0, loadedDocument.PageCount())
	indicators := make([]*thumbnailDropIndicator, 0, loadedDocument.PageCount())
	thumbImages := make([]*canvas.Image, 0, loadedDocument.PageCount())
	thumbBorders := make([]*canvas.Rectangle, 0, loadedDocument.PageCount())
	thumbLoaded := make([]bool, loadedDocument.PageCount())
	thumbLoading := make([]bool, loadedDocument.PageCount())
	pageImages := make([]*canvas.Image, 0, loadedDocument.PageCount())
	pageBorders := make([]*canvas.Rectangle, 0, loadedDocument.PageCount())
	pageSizes := make([]image.Point, 0, loadedDocument.PageCount())
	pageLoaded := make([]bool, loadedDocument.PageCount())
	pageLoading := make([]bool, loadedDocument.PageCount())
	signatureLayers := make(map[int]*signatureLayer, loadedDocument.PageCount())
	g.formLayers = make(map[int]*formLayer, loadedDocument.PageCount())
	panOverlays := make([]*panOverlay, 0, loadedDocument.PageCount())

	firstPreview, err := loadedDocument.Preview(0)
	if err != nil {
		dialog.ShowError(err, g.window)
		return
	}
	for page := 0; page < loadedDocument.PageCount(); page++ {
		pageSize, err := loadedDocument.PageSize(page)
		if err != nil {
			dialog.ShowError(err, g.window)
			return
		}
		var preview image.Image
		if page == 0 {
			preview = firstPreview
			pageLoaded[page] = true
			thumbLoaded[page] = true
		}
		pageImage := canvas.NewImageFromImage(preview)
		// Let the GPU scale the preview instead of resampling pixels on the UI thread.
		pageImage.ScaleMode = canvas.ImageScaleFastest
		pageImage.FillMode = canvas.ImageFillContain
		pageImage.SetMinSize(fyne.NewSize(float32(pageSize.X), float32(pageSize.Y)))
		pageImages = append(pageImages, pageImage)
		pageSizes = append(pageSizes, pageSize)
		pageBorder := canvas.NewRectangle(color.Transparent)
		pageBorder.StrokeColor = theme.PrimaryColor()
		pageBorder.StrokeWidth = 3
		if page != 0 {
			pageBorder.Hide()
		}
		pageBorders = append(pageBorders, pageBorder)

		layer := newSignatureLayer(loadedDocument, page, pageSize, g.selectSignature, g.activatePDFPage, g.documentChanged)
		g.bindAnnotationLayer(layer)
		signatureLayers[page] = layer
		pan := newPanOverlay(g.pdfScroll)
		panOverlays = append(panOverlays, pan)
		crop := newCropOverlay()
		crop.onActiveChanged = g.setCropToolActive
		g.pdfCrops[page] = crop
		forms := newFormLayer(g, page)
		g.formLayers[page] = forms
		pageContainer := container.NewCenter(container.NewStack(canvas.NewRectangle(color.White), pageImage, pageBorder, layer, crop, forms, pan))
		objects = append(objects, pageContainer)
		pageContainers = append(pageContainers, pageContainer)

		thumbImage := canvas.NewImageFromImage(preview)
		thumbImage.FillMode = canvas.ImageFillContain
		thumbImage.SetMinSize(thumbnailSizeForSidebar(pageSize, g.sidebarSplit.width))
		thumbImages = append(thumbImages, thumbImage)
		thumbBorder := canvas.NewRectangle(color.Transparent)
		thumbBorder.StrokeColor = theme.PrimaryColor()
		thumbBorder.StrokeWidth = 3
		if page != 0 {
			thumbBorder.Hide()
		}
		thumbBorders = append(thumbBorders, thumbBorder)

		index := page
		goToPage := newThumbnailTap(func() {
			if g.working {
				return
			}
			g.loadPDFPage(index)
			g.activatePDFPage(index)
			g.pdfScroll.ScrollToOffset(fyne.NewPos(g.pdfScroll.Offset.X, g.pdfPageTop(index)))
		})

		goToPage.onDrag = func(event *fyne.DragEvent) { g.dragThumbnail(index, loadedDocument, generation, event) }
		goToPage.onMouseDown = func(event *desktop.MouseEvent) { g.pressThumbnail(index, loadedDocument, generation, event) }
		goToPage.onMouseUp = g.releaseThumbnail
		goToPage.onCursor = g.thumbnailCursor
		goToPage.onDragEnd = g.finishThumbnailDrag
		goToPage.onSecondary = func(event *fyne.PointEvent) { g.thumbnailContextMenu(index, event) }
		indicator := newThumbnailDropIndicator()
		indicators = append(indicators, indicator)
		card := container.NewVBox(
			container.NewStack(canvas.NewRectangle(color.White), thumbImage, thumbBorder),
			widget.NewLabel(fmt.Sprintf("Page %d", page+1)),
		)
		thumbs = append(thumbs, container.NewStack(card, indicator, goToPage))
	}

	g.clearSignatureSelection()
	g.activePDFPage = 0
	g.signatureLayers = signatureLayers
	g.panOverlays = panOverlays
	g.pdfThumbDropIndicators = indicators
	g.pdfThumbImages = thumbImages
	g.pdfThumbBorders = thumbBorders
	g.pdfThumbLoaded = thumbLoaded
	g.pdfThumbLoading = thumbLoading
	g.pdfPageImages = pageImages
	g.pdfPageBorders = pageBorders
	g.pdfPageSizes = pageSizes
	g.pdfPageLoaded = pageLoaded
	g.pdfPageLoading = pageLoading
	g.pdfPageContainers = pageContainers
	g.imageSignatureLayer.Hide()
	g.cropOverlay.Deactivate()
	g.pdfPages.Objects = objects
	g.thumbList.Objects = thumbs
	g.pdfPages.Refresh()
	g.thumbList.Refresh()
	g.image.Hide()
	g.imageScroll.Hide()
	g.image.Image = nil
	g.pdfScroll.Show()
	g.sidebar.container.Show()
	g.pdfScroll.ScrollToTop()
	g.setDocument(loadedDocument)
	g.resetZoom()
	g.loadVisiblePDFPages()
	g.loadVisiblePDFThumbnails()
	g.updateTitle()
	g.updateHistoryActions()
}

func (g *Gui) setDocument(doc *document.Document) {
	g.stopFormPulse()
	previous := g.document
	if previous != doc {
		g.stopOCR()
		g.objectPasteDocument = nil
		g.objectPasteCount = 0
	}
	g.document = doc
	g.fileSizeBytes = doc.SourceSize()
	g.clearRedactionSelection()
	g.redactedTargets = nil
	g.setAnnotationTool(toolSelect)
	if previous != doc {
		g.startFormPulse()
	}
	if previous != nil && previous != doc {
		go previous.Close()
	}
}
