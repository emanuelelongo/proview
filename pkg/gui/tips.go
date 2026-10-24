package gui

import (
	"emanuelelongo/proview/pkg/document"
	_ "embed"
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

const (
	startupTipDelay   = 5 * time.Second
	tipDuration       = 8 * time.Second
	tipGap            = 8 * time.Second
	tipsPreferenceKey = "tips.enabled"
	tipFadeStep       = 100 * time.Millisecond
	tipFadeSteps      = 16
)

// One tip per line; blank lines and comments are ignored for easier review.
//
//go:embed tips.txt
var tipsText string

var startupTips = loadTips(tipsText)

func loadTips(text string) []string {
	var tips []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			tips = append(tips, line)
		}
	}
	return tips
}

// startTipSequence is called only for the application's first editor window.
// Additional windows opened during the same run do not repeat the sequence.
func (g *Gui) startTipSequence() {
	if g.tipsEnabled {
		g.restartTipSequence()
	}
}

func (g *Gui) restartTipSequence() {
	if g.tipBar == nil || g.tipText == nil || len(startupTips) == 0 || g.closed || !g.tipsEnabled {
		return
	}
	g.stopTipSequence()
	g.tipIndex = 0
	generation := g.tipGeneration
	g.scheduleTip(startupTipDelay, generation, func() { g.showNextTip(generation, true) })
}

func (g *Gui) scheduleTip(delay time.Duration, generation uint64, action func()) {
	if g.selectionStatusActive {
		g.resumeTipsAfterSelection = true
		return
	}
	g.tipTimer = time.AfterFunc(delay, func() {
		fyne.Do(func() {
			if g.closed || g.selectionStatusActive || !g.tipsEnabled || generation != g.tipGeneration {
				return
			}
			g.tipTimer = nil
			action()
		})
	})
}

func (g *Gui) showNextTip(generation uint64, schedule bool) {
	if g.closed || g.selectionStatusActive || !g.tipsEnabled || generation != g.tipGeneration || len(startupTips) == 0 {
		return
	}
	g.tipText.Color = theme.Color(theme.ColorNameForeground)
	g.tipText.Text = startupTips[g.tipIndex%len(startupTips)]
	g.refreshTipText()
	g.tipBar.Show()
	g.tipIndex = (g.tipIndex + 1) % len(startupTips)
	if schedule {
		g.fadeTip(generation, 1, time.Now().Add(tipDuration))
	}
}

func (g *Gui) finishCurrentTip(generation uint64, schedule bool) {
	if g.closed || g.selectionStatusActive || !g.tipsEnabled || generation != g.tipGeneration {
		return
	}
	g.hideTip()
	if schedule {
		g.scheduleTip(tipGap, generation, func() { g.showNextTip(generation, true) })
	}
}

func (g *Gui) hideTip() {
	if g.selectionStatusActive || g.tipText == nil {
		return
	}
	g.tipText.Text = ""
	g.refreshTipText()
}

// Animate only the text opacity: sixteen small updates, no layout or image refresh.
func (g *Gui) fadeTip(generation uint64, step int, deadline time.Time) {
	if g.closed || g.selectionStatusActive || !g.tipsEnabled || generation != g.tipGeneration {
		return
	}
	foreground := color.NRGBAModel.Convert(theme.Color(theme.ColorNameForeground)).(color.NRGBA)
	foreground.A = uint8(int(foreground.A) * step / tipFadeSteps)
	g.tipText.Color = foreground
	g.tipText.Refresh()
	if step < tipFadeSteps {
		g.scheduleTip(tipFadeStep, generation, func() { g.fadeTip(generation, step+1, deadline) })
	} else {
		g.scheduleTip(max(0, time.Until(deadline)), generation, func() { g.finishCurrentTip(generation, true) })
	}
}

// Text width must not change the editor's minimum size when tips rotate.
type tipTextLayout struct{}

func (tipTextLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, 22) }
func (tipTextLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		textSize := object.MinSize()
		if text, ok := object.(*canvas.Text); ok && text.Alignment == fyne.TextAlignTrailing {
			textSize.Width = max(0, size.Width-8)
		}
		object.Move(fyne.NewPos(4, (size.Height-textSize.Height)/2))
		object.Resize(textSize)
	}
}

// Keep file information on the right and clip tips to the remaining width.
type statusBarLayout struct{}

func (statusBarLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, 22) }
func (statusBarLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}
	infoWidth := min(float32(360), size.Width*.4)
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(max(0, size.Width-infoWidth-8), size.Height))
	objects[1].Move(fyne.NewPos(size.Width-infoWidth, 0))
	objects[1].Resize(fyne.NewSize(infoWidth, size.Height))
}
func (g *Gui) updateFileInfo() {
	if g.fileInfo == nil {
		return
	}
	text := ""
	if g.document != nil {
		sizeBytes := g.fileSizeBytes
		if sizeBytes == 0 {
			sizeBytes = g.document.SourceSize()
		}
		megabytes := float64(sizeBytes) / 1_000_000
		if g.document.Format() == document.FormatPDF {
			text = fmt.Sprintf("Page %d/%d · %.2f MB", g.activePDFPage+1, g.document.PageCount(), megabytes)
		} else if size, err := g.document.PageSize(0); err == nil {
			text = fmt.Sprintf("%d × %d px · %.2f MB", size.X, size.Y, megabytes)
		}
	}
	if g.showingModel() {
		text = g.modelInfo()
	}
	if g.showingMarkdown() {
		text = g.markdownView.doc.Info()
	}
	if text != g.fileInfo.Text {
		g.fileInfo.Text = text
		g.fileInfo.Refresh()
	}
}

func (g *Gui) stopTipSequence() {
	if g.tipTimer != nil {
		g.tipTimer.Stop()
		g.tipTimer = nil
	}
	g.tipGeneration++
	g.hideTip()
}

func (g *Gui) setTipsEnabled(enabled bool) {
	g.tipsEnabled = enabled
	g.app.Preferences().SetBool(tipsPreferenceKey, enabled)
	if g.tipsItem != nil {
		g.tipsItem.Checked = enabled
		refreshMenuState(g.tipsMenu)
	}
	if enabled {
		g.restartTipSequence()
	} else {
		g.stopTipSequence()
	}
}

// Selection text owns the tip slot until deselection. Invalidate pending fade
// callbacks so they cannot clear or dim the selected text after the timer stops.
func (g *Gui) showTextSelectionStatus() {
	if g.tipText == nil || !g.validTextSelection() {
		return
	}
	if !g.selectionStatusActive {
		g.selectionStatusActive = true
		g.resumeTipsAfterSelection = g.tipTimer != nil
		g.stopTipSequence()
	}
	s := g.textSelection
	g.tipText.Color = theme.Color(theme.ColorNameForeground)
	if g.selectedTextURL() != nil {
		g.tipText.Color = theme.Color(theme.ColorNameHyperlink)
	}
	g.tipText.Text = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s.layout.Text(s.start, s.end))
	g.refreshTipText()
	if g.tipBar != nil {
		g.tipBar.Show()
	}
}

func (g *Gui) clearTextSelectionStatus() {
	if !g.selectionStatusActive {
		return
	}
	g.selectionStatusActive = false
	resume := g.resumeTipsAfterSelection
	g.resumeTipsAfterSelection = false
	g.hideTip()
	if resume && g.tipsEnabled && !g.closed {
		generation := g.tipGeneration
		g.scheduleTip(tipGap, generation, func() { g.showNextTip(generation, true) })
	}
}

func (g *Gui) refreshTipText() {
	g.tipText.Resize(g.tipText.MinSize())
	g.tipText.Refresh()
	if g.tipSurface != nil {
		g.tipSurface.Resize(g.tipText.MinSize())
		g.tipSurface.Refresh()
	}
}
