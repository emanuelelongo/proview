package gui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"emanuelelongo/proview/pkg/document"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	pdfUniformPages    = "Same page size"
	pdfIndividualPages = "Individual page sizes"
	pdfPaperA4         = "A4 (210 × 297 mm)"
	pdfPaperLetter     = "US Letter (8.5 × 11 in)"
	pdfPaperCustom     = "Custom"
)

// Reserve the source selection while choosing its output layout, just as the
// print page chooser does. Cancel releases queued file openings without work.
func (g *Gui) choosePDFLayout(sources []selectedPageSource) {
	g.working = true
	mode := widget.NewRadioGroup([]string{pdfUniformPages, pdfIndividualPages}, nil)
	mode.Required = true
	paper := widget.NewSelect([]string{pdfPaperA4, pdfPaperLetter, pdfPaperCustom}, nil)
	orientation := widget.NewSelect([]string{"Portrait", "Landscape"}, nil)
	orientation.SetSelected("Portrait")
	width, height := widget.NewEntry(), widget.NewEntry()
	width.SetText("210")
	height.SetText("297")
	validateDimension := func(text string) error { _, err := parsePDFDimension(text); return err }
	width.Validator = validateDimension
	height.Validator = validateDimension
	paperRow := widget.NewForm(widget.NewFormItem("Paper", paper))
	orientationRow := widget.NewForm(widget.NewFormItem("Orientation", orientation))
	customRows := widget.NewForm(widget.NewFormItem("Width (mm)", width), widget.NewFormItem("Height (mm)", height))
	settings := container.NewVBox(paperRow, orientationRow, customRows)
	help := widget.NewLabel("")
	help.Wrapping = fyne.TextWrapWord
	update := func() {
		if mode.Selected == pdfIndividualPages {
			settings.Hide()
			help.SetText("Each image fills its page without added margins. PDF and Markdown pages keep their sizes.")
			return
		}
		settings.Show()
		help.SetText("Fit all content to identical pages, preserving proportions and without cropping.")
		if paper.Selected == pdfPaperCustom {
			customRows.Show()
			orientationRow.Hide()
		} else {
			customRows.Hide()
			orientationRow.Show()
		}
	}
	mode.OnChanged = func(string) { update() }
	paper.OnChanged = func(string) { update() }
	paper.SetSelected(pdfPaperA4)
	mode.SetSelected(pdfUniformPages)
	prompt := dialog.NewCustomWithoutButtons("Create PDF", container.NewVBox(mode, help, settings), g.window)
	handled := false
	prompt.SetOnClosed(func() {
		if handled {
			return
		}
		handled = true
		g.working = false
		if !g.closed {
			g.processPendingFiles()
		}
	})
	create := widget.NewButton("Create PDF", func() {
		if handled || g.closed {
			return
		}
		var layout *document.PDFPageLayout
		if mode.Selected == pdfUniformPages {
			value := document.PDFPageLayout{WidthMM: 210, HeightMM: 297}
			switch paper.Selected {
			case pdfPaperLetter:
				value.WidthMM, value.HeightMM = 215.9, 279.4
			case pdfPaperCustom:
				var err error
				value.WidthMM, err = parsePDFDimension(width.Text)
				if err != nil {
					width.SetValidationError(err)
					return
				}
				value.HeightMM, err = parsePDFDimension(height.Text)
				if err != nil {
					height.SetValidationError(err)
					return
				}
			}
			if paper.Selected != pdfPaperCustom && orientation.Selected == "Landscape" {
				value.WidthMM, value.HeightMM = value.HeightMM, value.WidthMM
			}
			layout = &value
		}
		handled = true
		prompt.Hide()
		g.working = false
		g.createSelectionPDF(sources, layout)
	})
	create.Importance = widget.HighImportance
	prompt.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", prompt.Hide), create})
	prompt.Show()
	prompt.Resize(fyne.NewSize(480, 360))
}

func parsePDFDimension(text string) (float64, error) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 10 || value > 2000 {
		return 0, fmt.Errorf("Enter a size between 10 and 2000 mm.")
	}
	return value, nil
}
