package gui

import (
	"bytes"
	"emanuelelongo/proview/pkg/annotations"
	"emanuelelongo/proview/pkg/document"
	"image"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	fyneTest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestDocumentActionsAndDirtyTitle(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := &Gui{
		app:             app,
		window:          app.NewWindow("Proview"),
		cropOverlay:     newCropOverlay(),
		signatureLayers: make(map[int]*signatureLayer),
	}
	BuildMenu(g)
	if !g.undoItem.Disabled || !g.redoItem.Disabled || !g.saveItem.Disabled || !g.saveAsItem.Disabled {
		t.Fatal("document actions are enabled without a document")
	}

	g.document = testDocument(t, 100, 50)
	g.updateHistoryActions()
	if g.saveItem.Disabled || g.saveAsItem.Disabled {
		t.Fatal("save actions are disabled with an open document")
	}
	if got := g.window.Title(); got != "image.png — Proview" {
		t.Fatalf("clean title = %q", got)
	}
	if _, err := g.document.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, testSignature(t, 2, 1)); err != nil {
		t.Fatal(err)
	}
	g.updateHistoryActions()
	if g.undoItem.Disabled || !g.redoItem.Disabled {
		t.Fatal("history menu state does not reflect the edit")
	}
	if got := g.window.Title(); got != "• image.png — Proview" {
		t.Fatalf("dirty title = %q", got)
	}
}

func TestTransformActionsRequireOpenDocument(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if len(g.transformActions) != 4 {
		t.Fatalf("transform action count = %d", len(g.transformActions))
	}
	for _, action := range g.transformActions {
		if !action.button.Disabled() {
			t.Fatal("transform action is enabled without a document")
		}
	}
	g.showDocument(testDocument(t, 100, 50))
	for _, action := range g.transformActions {
		if action.button.Disabled() {
			t.Fatal("transform action stayed disabled with an open document")
		}
	}
}

func TestStartupTipsReserveSpaceAndCycle(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	if !g.tipBar.Visible() || g.tipText.Text != "" {
		t.Fatal("tip slot must be visible and initially empty")
	}
	if g.tipText.Alignment != fyne.TextAlignLeading {
		t.Fatal("tip text is not left aligned")
	}
	g.window.Content().Refresh()
	initialSize := g.tipBar.MinSize()
	viewport := g.imageScroll.Size()
	g.tipGeneration++
	generation := g.tipGeneration
	for index := 0; index < len(startupTips)*3; index++ {
		g.showNextTip(generation, false)
		if g.tipText.Text != startupTips[index%len(startupTips)] {
			t.Fatal("tip sequence did not cycle")
		}
		g.window.Content().Refresh()
		if g.tipBar.MinSize() != initialSize || g.imageScroll.Size() != viewport {
			t.Fatal("showing a tip changed the reserved space or viewport")
		}
		g.finishCurrentTip(generation, false)
		if !g.tipBar.Visible() || g.tipText.Text != "" || g.tipBar.MinSize() != initialSize {
			t.Fatal("empty tip did not retain its reserved space")
		}
	}
}

func TestTipsMenuTogglePersistsAndRestartsWithDelay(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	defer g.stopTipSequence()
	if g.tipsItem == nil || !g.tipsItem.Checked || !g.tipsEnabled {
		t.Fatal("tips must default to enabled")
	}
	g.startTipSequence()
	if g.tipTimer == nil || g.tipText.Text != "" {
		t.Fatal("startup must wait before showing the first tip")
	}
	generation := g.tipGeneration
	g.showNextTip(generation, false)
	g.tipsItem.Action()
	if g.tipsEnabled || g.tipsItem.Checked || g.tipTimer != nil || g.tipText.Text != "" || !g.tipBar.Visible() {
		t.Fatal("disabling tips did not clear the sequence while retaining the bar")
	}
	g.showNextTip(generation, false)
	if g.tipText.Text != "" {
		t.Fatal("obsolete timer restored disabled tips")
	}
	next := newGui(app)
	if next.tipsEnabled || next.tipsItem.Checked {
		t.Fatal("disabled preference was not restored")
	}
	next.startTipSequence()
	if next.tipTimer != nil {
		t.Fatal("disabled tips started at startup")
	}
	g.tipsItem.Action()
	if !g.tipsEnabled || !g.tipsItem.Checked || g.tipTimer == nil || g.tipText.Text != "" {
		t.Fatal("enabling tips must restart with a delay")
	}
	if !app.Preferences().Bool(tipsPreferenceKey) {
		t.Fatal("enabled preference was not saved")
	}
	g.stopTipSequence()
	g.showNextTip(g.tipGeneration, false)
	g.finishCurrentTip(g.tipGeneration, true)
	if g.tipText.Text != "" || g.tipTimer == nil {
		t.Fatal("tips must remain blank during the scheduled gap")
	}
}

func TestThirdPartyLicensesAreReadableAndComplete(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	var licenses *fyne.MenuItem
	for _, menu := range g.window.MainMenu().Items {
		if menu.Label != "Help" {
			continue
		}
		for _, item := range menu.Items {
			if item.Label == "Third-Party Licenses" {
				licenses = item
			}
		}
	}
	if licenses == nil {
		t.Fatal("Help menu has no third-party licenses action")
	}
	licenses.Action()
	var notice *widget.Label
	for _, object := range fyneTest.LaidOutObjects(g.window.Canvas().Overlays().Top()) {
		if label, ok := object.(*widget.Label); ok && strings.HasPrefix(label.Text, "THIRD-PARTY COMPONENTS") {
			notice = label
			break
		}
	}
	if notice == nil || notice.Importance == widget.LowImportance || !notice.Selectable {
		t.Fatal("license notice is missing, dimmed, or not selectable")
	}
	for _, expected := range []string{"Fyne — BSD 3-Clause", "Tabler Icons — MIT", "Go Sans / Go Mono", "GNU AFFERO GENERAL PUBLIC LICENSE"} {
		if !strings.Contains(notice.Text, expected) {
			t.Fatalf("license notice is missing %q", expected)
		}
	}
	tapDialogButton(t, g, "Close")
}

func TestShapeSelectorShowsDropdownIndicator(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	selector, ok := g.shapeSelector(annotations.Rectangle, func(annotations.ShapeKind) {}).(*fyne.Container)
	if !ok || len(selector.Objects) != 2 {
		t.Fatal("shape selector has no dropdown indicator")
	}
	selector.Resize(fyne.NewSize(44, 36))
	arrow := selector.Objects[1]
	if arrow.Position() != fyne.NewPos(32, 24) || arrow.Size() != fyne.NewSize(10, 10) {
		t.Fatalf("dropdown indicator layout = %v %v", arrow.Position(), arrow.Size())
	}
}

func TestPDFFlipActionsRequireSelectedObject(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	source, err := document.Load("page.pdf", bytes.NewReader(scrollTestPDF(1)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := document.New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	g.document = doc
	g.updateTransformActions()
	for _, action := range g.flipActions {
		if !action.button.Disabled() {
			t.Fatal("whole-page PDF flip is enabled")
		}
	}
	g.hasSelectedSignature = true
	g.updateTransformActions()
	for _, action := range g.flipActions {
		if action.button.Disabled() {
			t.Fatal("PDF object flip is disabled with a selected object")
		}
	}
}

func TestEscapeCancelsCrop(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	crop := newCropOverlay()
	crop.Activate(image.Rect(0, 0, 100, 100), nil)
	g := &Gui{cropOverlay: crop, signatureLayers: make(map[int]*signatureLayer)}
	if !g.cancelCurrentOperation() || crop.active || crop.Visible() {
		t.Fatal("Escape did not cancel and hide the active crop")
	}
	if g.cancelCurrentOperation() {
		t.Fatal("inactive editor operation reported as cancelled")
	}
}

func TestCropToolShowsActiveStateAndCrosshair(t *testing.T) {
	action := newToggleToolbarAction(theme.ContentCutIcon(), nil)
	crop := newCropOverlay()
	selection := newToggleToolbarAction(theme.DocumentIcon(), nil)
	selection.SetActive(true)
	g := &Gui{cropAction: action, annotationTool: toolSelect, annotationActions: map[annotationTool]*toggleToolbarAction{toolSelect: selection}}
	crop.onActiveChanged = g.setCropToolActive

	crop.Activate(image.Rect(0, 0, 100, 100), nil)
	if action.button.Importance != widget.HighImportance {
		t.Fatal("crop toolbar action is not highlighted while active")
	}
	if selection.button.Importance == widget.HighImportance {
		t.Fatal("selection stayed highlighted while crop was active")
	}
	if crop.Cursor() != desktop.CrosshairCursor {
		t.Fatalf("crop cursor = %v, want crosshair", crop.Cursor())
	}
	crop.Deactivate()
	if action.button.Importance != widget.LowImportance {
		t.Fatal("crop toolbar action stayed highlighted after deactivation")
	}
	if selection.button.Importance != widget.HighImportance {
		t.Fatal("selection was not restored after crop ended")
	}
}

func TestMenuDeclaresEditingShortcuts(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := &Gui{
		app:             app,
		window:          app.NewWindow("Proview"),
		cropOverlay:     newCropOverlay(),
		signatureLayers: make(map[int]*signatureLayer),
	}
	BuildMenu(g)
	if _, ok := g.undoItem.Shortcut.(*fyne.ShortcutUndo); !ok {
		t.Fatalf("undo shortcut = %T", g.undoItem.Shortcut)
	}
	if _, ok := g.redoItem.Shortcut.(*fyne.ShortcutRedo); !ok {
		t.Fatalf("redo shortcut = %T", g.redoItem.Shortcut)
	}
}

func TestDeleteKeyRemovesSelectedSignature(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	doc := testDocument(t, 100, 50)
	signature, err := doc.AddSignature(0, document.NormalizedRect{X: .1, Y: .1, Width: .2, Height: .2}, testSignature(t, 2, 1))
	if err != nil {
		t.Fatal(err)
	}
	layer := newSignatureLayer(doc, 0, image.Pt(100, 50), nil, nil, nil)
	g := &Gui{
		app:                   app,
		window:                app.NewWindow("Proview"),
		document:              doc,
		cropOverlay:           newCropOverlay(),
		signatureLayers:       map[int]*signatureLayer{0: layer},
		selectedSignature:     signature.ID,
		selectedSignaturePage: 0,
		hasSelectedSignature:  true,
	}
	g.installKeyHandler()

	g.window.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyDelete})
	if len(doc.Signatures()) != 0 {
		t.Fatal("Delete did not remove the selected signature")
	}
	if g.hasSelectedSignature {
		t.Fatal("signature selection remained active after deletion")
	}
}

func TestLargerIconThemeOnlyChangesInlineIconSize(t *testing.T) {
	base := theme.DefaultTheme()
	larger := largerIconTheme{Theme: base}
	if got, want := larger.Size(theme.SizeNameInlineIcon), base.Size(theme.SizeNameInlineIcon)+iconSizeIncrease; got != want {
		t.Fatalf("inline icon size = %f, want %f", got, want)
	}
	if got, want := larger.Size(theme.SizeNameText), base.Size(theme.SizeNameText); got != want {
		t.Fatalf("text size = %f, want unchanged %f", got, want)
	}
}

func TestFileInfoTracksImageEditsWithoutChangingStatusBarSize(t *testing.T) {
	app := fyneTest.NewApp()
	defer app.Quit()
	g := newGui(app)
	slot := g.tipBar.MinSize()
	g.document = testDocument(t, 120, 80)
	defer g.document.Close()
	g.updateFileInfo()
	if g.fileInfo.Text != "120 × 80 px · 0.00 MB" {
		t.Fatalf("file info = %q", g.fileInfo.Text)
	}
	if err := g.document.ApplyCrop(image.Rect(0, 0, 60, 40), image.Rect(0, 0, 120, 80)); err != nil {
		t.Fatal(err)
	}
	g.updateFileInfo()
	if g.fileInfo.Text != "60 × 40 px · 0.00 MB" {
		t.Fatalf("cropped file info = %q", g.fileInfo.Text)
	}
	g.fileSizeBytes = 1_250_000
	g.updateFileInfo()
	if g.fileInfo.Text != "60 × 40 px · 1.25 MB" {
		t.Fatalf("file size = %q", g.fileInfo.Text)
	}
	if g.fileInfo.TextSize != g.tipText.TextSize {
		t.Fatal("status texts have different font sizes")
	}
	if g.tipBar.MinSize() != slot {
		t.Fatal("file info changed status bar size")
	}
	g.document = nil
	g.updateFileInfo()
	if g.fileInfo.Text != "" {
		t.Fatal("file info remains without a document")
	}
}
