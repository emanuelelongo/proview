package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const iconSizeIncrease = float32(3)

type largerIconTheme struct {
	fyne.Theme
}

func (t largerIconTheme) Size(name fyne.ThemeSizeName) float32 {
	size := t.Theme.Size(name)
	if name == theme.SizeNameInlineIcon {
		return size + iconSizeIncrease
	}
	return size
}
