package gui

import (
	"emanuelelongo/proview/assets"
	"emanuelelongo/proview/internal/appprofile"
	"emanuelelongo/proview/internal/fileassoc"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	img "emanuelelongo/proview/pkg/images"
	"emanuelelongo/proview/pkg/signatures"
	"image"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type Gui struct {
	editors                           *editorGroup
	externalOpenPauses                int
	documents                         []*editorDocument
	activeDocument                    *editorDocument
	documentList                      *fyne.Container
	pendingFiles                      []string
	openingFiles                      bool
	pdfOpenDialog                     *dialog.CustomDialog
	formPulse                         *fyne.Animation
	formPulseSerial                   uint64
	formsActive                       bool
	formAction                        *toggleToolbarAction
	formLayers                        map[int]*formLayer
	textEditing                       bool
	textEditAction                    *toggleToolbarAction
	textEditItem                      *fyne.MenuItem
	correctionLayers                  map[int]*textCorrectionLayer
	correctionDialog                  *dialog.CustomDialog
	textMove                          *textMoveState
	modelViewport                     *modelViewport
	modelPanel                        fyne.CanvasObject
	modelCrop                         *cropOverlay
	modelMenu                         *fyne.Menu
	modelMenuItems                    []*fyne.MenuItem
	modelWireframeItem                *fyne.MenuItem
	ocr                               *ocrState
	qr                                *qrState
	textSelection                     *textSelection
	signatureDialog                   *dialog.CustomDialog
	signaturePopup                    *widget.PopUp
	signatureDialogOverlay            fyne.CanvasObject
	signatureEscapeHandled            bool
	objectPasteDigest                 [32]byte
	objectPasteDocument               *document.Document
	objectPastePage, objectPasteCount int
	filePicker                        *filePickerState
	filePickerBackend                 filePickerBackend
	filePickerDirectory               string
	workDir                           string
	closed                            bool
	thumbnailDrag                     *thumbnailDragState
	thumbnailPress                    *thumbnailDragState
	pdfThumbDropIndicators            []*thumbnailDropIndicator
	thumbnailSelectionDocument        *document.Document
	thumbnailSelection                map[uint64]bool
	thumbnailAnchor                   uint64
	thumbnailSelectionFrozen          bool
	pdfGeneration                     uint64
	pdfDetail                         *pdfDetailState
	pdfBasePreviews                   *pdfBasePreviewState
	thumbnailRefresh                  *thumbnailRefreshState
	pdfPageIDs                        []uint64
	pdfPageTops                       []float32
	pdfKeepStart, pdfKeepEnd          int
	pdfThumbKeepStart                 int
	pdfThumbKeepEnd                   int
	pdfResident, pdfThumbResident     map[int]bool
	pdfBaseKeys                       map[int]document.PageBaseKey
	pdfLoadSlots                      chan struct{}
	pdfPageJobs, pdfThumbJobs         map[int]*pdfRasterJob
	pdfCrops                          map[int]*cropOverlay
	app                               fyne.App
	window                            fyne.Window
	image                             *canvas.Image
	imageContent                      *fyne.Container
	imageScroll                       *container.Scroll
	cropOverlay                       *cropOverlay
	cropAction                        *toggleToolbarAction
	openAction                        *iconToolbarAction
	blankAction                       *iconToolbarAction
	signatureAction                   *iconToolbarAction
	transformActions                  []*iconToolbarAction
	flipActions                       []*iconToolbarAction
	zoomOutAction                     *iconToolbarAction
	zoomInAction                      *iconToolbarAction
	imagePanOverlay                   *panOverlay
	panOverlays                       []*panOverlay
	mainSplit                         fyne.CanvasObject
	sidebarSplit                      *fixedSidebarSplit
	thumbList                         *fyne.Container
	pdfPages                          *fyne.Container
	pdfScroll                         *container.Scroll
	sidebar                           *Sidebar
	document                          *document.Document
	imageSignatureLayer               *signatureLayer
	signatureLayers                   map[int]*signatureLayer
	pdfThumbImages                    []*canvas.Image
	pdfThumbPlaceholders              []fyne.CanvasObject
	pdfThumbBorders                   []*canvas.Rectangle
	pdfThumbLoaded                    []bool
	pdfThumbLoading                   []bool
	pdfPageImages                     []*canvas.Image
	pdfPageBorders                    []*canvas.Rectangle
	pdfPageSizes                      []image.Point
	pdfPageLoaded                     []bool
	pdfPageLoading                    []bool
	pdfPageContainers                 []fyne.CanvasObject
	activePDFPage                     int
	selectedSignature                 document.SignatureID
	selectedSignaturePage             int
	hasSelectedSignature              bool
	selectedRedaction                 document.RedactionID
	selectedRedactionPage             int
	redactedTargets                   map[string][]string
	editMenu                          *fyne.Menu
	stateMenus                        []*fyne.Menu
	menuDocumentItems                 []*fyne.MenuItem
	undoItem                          *fyne.MenuItem
	redoItem                          *fyne.MenuItem
	saveItem                          *fyne.MenuItem
	saveAsItem                        *fyne.MenuItem
	signatureItem                     *fyne.MenuItem
	saveTarget                        fyne.URI
	objectOrderMenu                   *widget.PopUpMenu
	zoom                              float32
	fitZoom                           float32
	zoomLevel                         int
	search                            *documentSearch
	panModifierDown                   bool
	working                           bool
	signatureStore                    *signatures.Store
	signatureCloud                    *signatureCloudService
	areaSelection                     *areaSelection
	textEntry                         *annotationEntry
	textPlacement                     *textPlacement
	lensGeneration                    uint64
	previewRenderer                   *document.PreviewRenderer
	annotationTool                    annotationTool
	temporarySelection                bool
	selectionShiftKeys                uint8
	selectionModifierTimer            *time.Timer
	annotationDefaults                map[annotations.Kind]annotations.Data
	annotationActions                 map[annotationTool]*toggleToolbarAction
	annotationControls                *fyne.Container
	annotationPropertyScroll          *container.Scroll
	toolPanel                         *fyne.Container
	toolPanelLayout                   *responsiveToolLayout
	annotationJobs                    chan func() func()
	annotationWork                    []func() func()
	annotationRunning                 bool
	annotationPending                 int
	highlightWork                     chan struct{}
	annotationPropertyGeneration      uint64
	annotationPreviewTimer            *time.Timer
	annotationPreviewWork             func()
	annotationAfterIdle               []func()
	annotationPreviewID               document.OverlayID
	tipBar                            *fyne.Container
	tipText                           *canvas.Text
	tipSurface                        *statusTextSurface
	fileInfo                          *canvas.Text
	fileSizeBytes                     int64
	tipsEnabled                       bool
	tipsItem                          *fyne.MenuItem
	tipsMenu                          *fyne.Menu
	tipTimer                          *time.Timer
	tipIndex                          int
	tipGeneration                     uint64
	selectionCopyMenu                 *widget.PopUpMenu
	selectionStatusActive             bool
	resumeTipsAfterSelection          bool
}

func NewGui() *Gui {
	if err := registerDefaultFileAssociations(); err != nil {
		log.Printf("Could not register file associations: %v", err)
	}
	g := newGui(app.NewWithID(appprofile.ID()))
	g.startTipSequence()
	return g
}

func newGui(application fyne.App) *Gui {
	g := &Gui{zoom: 1, fitZoom: 1, app: application, tipsEnabled: application.Preferences().BoolWithFallback(tipsPreferenceKey, true)}
	g.editors = &editorGroup{windows: []*Gui{g}, active: g}
	g.app.SetIcon(fyne.NewStaticResource("app-icon.png", assets.AppIcon))
	if _, ok := g.app.Settings().Theme().(largerIconTheme); !ok {
		g.app.Settings().SetTheme(largerIconTheme{Theme: g.app.Settings().Theme()})
	}

	g.image = canvas.NewImageFromImage(nil)
	// Let the GPU scale the preview instead of resampling pixels on the UI thread.
	g.image.ScaleMode = canvas.ImageScaleFastest
	g.image.FillMode = canvas.ImageFillContain

	g.pdfPages = container.NewVBox()

	g.pdfScroll = container.NewScroll(g.pdfPages)
	g.pdfScroll.OnScrolled = func(fyne.Position) { g.loadVisiblePDFPages() }
	g.pdfScroll.Hide()

	g.thumbList = container.NewVBox()
	g.documentList = container.NewVBox()

	g.sidebar = NewSidebar(g)
	g.sidebar.container.OnScrolled = func(fyne.Position) { g.loadVisiblePDFThumbnails() }

	g.cropOverlay = newCropOverlay()
	g.cropOverlay.onActiveChanged = g.setCropToolActive
	g.signatureLayers = make(map[int]*signatureLayer)
	g.imageSignatureLayer = newSignatureLayer(nil, 0, image.Point{}, g.selectSignature, g.activatePDFPage, g.documentChanged)
	g.imagePanOverlay = newPanOverlay(nil)
	g.panOverlays = []*panOverlay{g.imagePanOverlay}
	g.imageContent = container.NewStack(g.image, g.imageSignatureLayer, g.cropOverlay, g.imagePanOverlay)
	g.imageScroll = container.NewScroll(g.imageContent)
	g.imagePanOverlay.SetScroll(g.imageScroll)

	g.formAction = newToggleToolbarAction(tablerIcon("forms"), g.toggleForms)
	g.formAction.button.tooltip = "Fill PDF form"
	g.formAction.preparePulse()
	g.textEditAction = newToggleToolbarAction(tablerIcon("file-type-pdf"), g.toggleTextEditing)
	g.textEditAction.button.tooltip = "Edit PDF Text"
	g.cropAction = newToggleToolbarAction(tablerIcon("crop"), g.startCrop)
	g.cropAction.button.tooltip = "Crop"
	g.annotationControls = container.NewVBox()
	g.annotationControls.Hide()
	g.annotationActions = map[annotationTool]*toggleToolbarAction{}
	for tool, icon := range map[annotationTool]string{toolSelect: "pointer", toolText: "text-outline", toolDraw: "sketching", toolHighlight: "highlight", toolRedact: "xxx", toolShapes: "circle-square"} {
		action := newToggleToolbarAction(tablerIcon(icon), func() { g.setAnnotationTool(tool) })
		action.button.tooltip = map[annotationTool]string{toolSelect: "Select", toolText: "Text", toolDraw: "Draw", toolHighlight: "Highlight", toolRedact: "Redact PDF", toolShapes: "Shapes"}[tool]
		g.annotationActions[tool] = action
	}
	g.annotationActions[toolSelect].SetActive(true)
	g.bindAnnotationLayer(g.imageSignatureLayer)
	g.openAction = newIconToolbarAction(tablerIcon("folder"), "Open file", g.requestOpen)
	g.blankAction = newIconToolbarAction(tablerIcon("file-spark"), "New page", g.newPage)
	g.signatureAction = newIconToolbarAction(tablerIcon("signature"), "Signature", g.promptSignature)
	g.zoomOutAction = newIconToolbarAction(tablerIcon("zoom-out"), "Zoom out", g.zoomOut)
	g.zoomInAction = newIconToolbarAction(tablerIcon("zoom-in"), "Zoom in", g.zoomIn)
	rotateLeftAction := newIconToolbarAction(tablerIcon("rotate-2"), "Rotate left", func() { g.transformSelected("left") })
	rotateRightAction := newIconToolbarAction(tablerIcon("rotate-clockwise-2"), "Rotate right", func() { g.transformSelected("right") })
	flipHorizontalAction := newIconToolbarAction(tablerIcon("flip-horizontal"), "Flip horizontally", func() { g.transformSelected("horizontal") })
	flipVerticalAction := newIconToolbarAction(tablerIcon("flip-vertical"), "Flip vertically", func() { g.transformSelected("vertical") })
	g.transformActions = []*iconToolbarAction{rotateLeftAction, rotateRightAction, flipHorizontalAction, flipVerticalAction}
	g.flipActions = []*iconToolbarAction{flipHorizontalAction, flipVerticalAction}
	toolbar := widget.NewToolbar(
		g.openAction,
		g.blankAction,
		g.annotationActions[toolSelect],
		g.cropAction,
		g.annotationActions[toolDraw],
		g.annotationActions[toolHighlight],
		g.annotationActions[toolShapes],
		g.annotationActions[toolText],
		g.signatureAction,
		g.formAction,
		g.textEditAction,
		g.annotationActions[toolRedact],
		widget.NewToolbarSeparator(),
		rotateLeftAction,
		rotateRightAction,
		flipHorizontalAction,
		flipVerticalAction,
		widget.NewToolbarSeparator(),
		g.zoomOutAction,
		g.zoomInAction,
	)

	g.modelPanel = newModelPanel(g)
	g.modelPanel.Hide()
	contentArea := container.NewStack(g.imageScroll, g.pdfScroll,
		newZoomScrollSurface(g, g.imageScroll), newZoomScrollSurface(g, g.pdfScroll), g.modelPanel)

	g.sidebarSplit = newFixedSidebarSplit(g.sidebar.panel, contentArea, defaultSidebarWidth, g.updatePDFThumbnailSizes)
	g.mainSplit = g.sidebarSplit

	properties := container.NewHScroll(g.annotationControls)
	properties.SetMinSize(fyne.NewSize(0, 85))
	g.annotationPropertyScroll = properties
	properties.Hide()
	g.tipText = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	g.tipText.TextSize = 12
	g.tipText.Alignment = fyne.TextAlignLeading
	tipBackground := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	// A fixed slot keeps both the viewport and window minimum size stable.
	tipContent := container.New(tipTextLayout{}, newStatusTextSurface(g))
	tipViewport := container.NewScroll(tipContent)
	tipViewport.Direction = container.ScrollNone
	g.fileInfo = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	g.fileInfo.TextSize = g.tipText.TextSize
	g.fileInfo.Alignment = fyne.TextAlignTrailing
	infoViewport := container.NewScroll(container.New(tipTextLayout{}, g.fileInfo))
	infoViewport.Direction = container.ScrollNone
	g.tipBar = container.NewStack(tipBackground, container.New(statusBarLayout{}, tipViewport, infoViewport))
	propertySeparator := widget.NewToolbarSeparator().ToolbarObject()
	g.toolPanelLayout = newResponsiveToolLayout(toolbar, properties, g.annotationControls, propertySeparator)
	g.toolPanelLayout.onModeChanged = func() {
		if g.window != nil && g.window.Content() != nil {
			g.window.Content().Refresh()
		}
	}
	g.toolPanel = container.New(g.toolPanelLayout, toolbar, propertySeparator, properties)
	documentArea := container.NewBorder(g.newSearchBar(), nil, nil, nil, g.mainSplit)
	g.search.host = documentArea
	layout := container.NewBorder(g.toolPanel, g.tipBar, nil, nil, documentArea)

	g.window = g.app.NewWindow("Proview")
	g.window.SetContent(newTooltipSurface(layout))
	g.window.Resize(fyne.NewSize(1000, 700))

	BuildMenu(g)
	g.window.SetCloseIntercept(g.requestClose)
	g.window.SetOnDropped(g.openDroppedFiles)
	g.installKeyHandler()

	return g
}

func (g *Gui) Run() {
	g.RunWithFiles(nil)
}

// RunWithFiles starts the application and queues paths in the document workspace.
func (g *Gui) RunWithFiles(paths []string) { g.RunWithOptions(paths, OpenOptions{}) }

// The initial launch already owns a fresh window; NewWindow affects forwarded
// requests only. Apply the dialog directory before showing the first window.
func (g *Gui) RunWithOptions(paths []string, options OpenOptions) {
	g.workDir = options.WorkDir
	// Showing the first window runs the native macOS launch event loop.
	// Capture document requests before AppKit starts processing them.
	registerOpenFilesHandler(nil)
	g.window.Show()
	g.openStartupFiles(paths)
	registerOpenFilesHandler(g.openFilesFromFinder)
	g.app.Run()
}

func (g *Gui) startCrop() {
	if g.working {
		return
	}
	if g.showingModel() {
		g.startModelCrop()
		return
	}
	if g.document == nil {
		return
	}
	if g.document != nil && g.document.Format() == document.FormatPDF {
		g.startPDFCrop()
		return
	}
	if g.cropOverlay.active {
		g.cropOverlay.Deactivate()
		return
	}
	if g.image.Image == nil {
		dialog.ShowInformation("No image", "Open an image before using crop.", g.window)
		return
	}
	g.clearSignatureSelection()
	g.setAnnotationTool(toolSelect)
	g.cropOverlay.Resize(g.image.Size())
	g.cropOverlay.Activate(g.image.Image.Bounds(), func(rect image.Rectangle) {
		if g.image.Image == nil || g.document == nil {
			return
		}
		if err := g.document.ApplyCrop(rect, g.image.Image.Bounds()); err != nil {
			g.showOperationError("crop", err)
			return
		}
		cropped := img.Crop(g.image.Image, rect)
		g.image.Image = cropped
		g.image.SetMinSize(fyne.NewSize(float32(cropped.Bounds().Dx()), float32(cropped.Bounds().Dy())))
		g.image.Refresh()
		g.imageSignatureLayer.SetPageSize(cropped.Bounds().Size())
		g.imageSignatureLayer.reload()
		g.applyZoom()
		g.updateHistoryActions()
	})
}

func (g *Gui) setCropToolActive(active bool) {
	for tool, action := range g.annotationActions {
		action.SetActive(!active && tool == g.effectiveAnnotationTool())
	}
	if g.cropAction != nil {
		g.cropAction.SetActive(active)
	}
}

// Custom profiles must not replace the installed application's file associations.
func registerDefaultFileAssociations() error {
	if appprofile.Root != "" {
		return nil
	}
	return fileassoc.Register(openFileExtensions())
}
