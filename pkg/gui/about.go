package gui

import (
	"emanuelelongo/proview/internal/buildinfo"
	"net/url"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (g *Gui) showAbout() {
	info := buildinfo.Current()
	label := func(text string) *widget.Label {
		value := widget.NewLabel(text)
		value.Selectable = true
		return value
	}
	repository, _ := url.Parse(info.Repository)
	content := widget.NewForm(
		widget.NewFormItem("Name", label(info.Name)),
		widget.NewFormItem("Version", label(info.Version)),
		widget.NewFormItem("Repository", widget.NewHyperlink(info.Repository, repository)),
		widget.NewFormItem("Commit", label(info.Commit)),
		widget.NewFormItem("Date", label(info.Date)),
	)
	dialog.NewCustom("About", "Close", content, g.window).Show()
}
