package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// responsiveToolLayout keeps the property controls together. They follow the
// main toolbar when the complete block fits, otherwise they move to row two.
type responsiveToolLayout struct {
	toolbar       fyne.CanvasObject
	properties    *container.Scroll
	controls      *fyne.Container
	separator     fyne.CanvasObject
	inline        bool
	refreshQueued bool
	onModeChanged func()
}

func newResponsiveToolLayout(toolbar fyne.CanvasObject, properties *container.Scroll, controls *fyne.Container, separator fyne.CanvasObject) *responsiveToolLayout {
	return &responsiveToolLayout{toolbar: toolbar, properties: properties, controls: controls, separator: separator}
}

func (l *responsiveToolLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	toolbarSize := l.toolbar.MinSize()
	if !l.properties.Visible() {
		return toolbarSize
	}
	if l.inline {
		return fyne.NewSize(toolbarSize.Width, l.inlineHeight())
	}
	propertyHeight := l.propertyHeight()
	return fyne.NewSize(toolbarSize.Width, toolbarSize.Height+theme.Padding()+propertyHeight)
}

func (l *responsiveToolLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	l.updateMode(size.Width)
	toolbarSize := l.toolbar.MinSize()
	if !l.properties.Visible() {
		l.toolbar.Move(fyne.NewPos(0, 0))
		l.toolbar.Resize(size)
		l.separator.Resize(fyne.NewSize(0, 0))
		return
	}
	if l.inline {
		gap := theme.Padding()
		rowHeight := l.inlineHeight()
		separatorWidth := max(float32(1), l.separator.MinSize().Width)
		separatorX := toolbarSize.Width + gap
		propertyX := separatorX + separatorWidth + gap
		l.toolbar.Move(fyne.NewPos(0, 0))
		l.toolbar.Resize(fyne.NewSize(toolbarSize.Width, rowHeight))
		l.separator.Move(fyne.NewPos(separatorX, 0))
		l.separator.Resize(fyne.NewSize(separatorWidth, rowHeight))
		l.properties.Move(fyne.NewPos(propertyX, 0))
		l.properties.Resize(fyne.NewSize(max(0, size.Width-propertyX), rowHeight))
		return
	}
	propertyHeight := l.propertyHeight()
	l.toolbar.Move(fyne.NewPos(0, 0))
	l.toolbar.Resize(fyne.NewSize(size.Width, toolbarSize.Height))
	l.separator.Resize(fyne.NewSize(0, 0))
	l.properties.Move(fyne.NewPos(0, toolbarSize.Height+theme.Padding()))
	l.properties.Resize(fyne.NewSize(size.Width, propertyHeight))
}

func (l *responsiveToolLayout) updateMode(width float32) {
	inline := false
	if l.properties.Visible() {
		separatorWidth := max(float32(1), l.separator.MinSize().Width)
		required := l.toolbar.MinSize().Width + 2*theme.Padding() + separatorWidth + l.controls.MinSize().Width
		inline = width >= required
	}
	if inline == l.inline {
		return
	}
	l.inline = inline
	if l.onModeChanged == nil || l.refreshQueued {
		return
	}
	l.refreshQueued = true
	fyne.Do(func() {
		l.refreshQueued = false
		l.onModeChanged()
	})
}

func (l *responsiveToolLayout) propertyHeight() float32 {
	return max(l.properties.MinSize().Height, l.controls.MinSize().Height+14)
}

func (l *responsiveToolLayout) inlineHeight() float32 {
	return max(l.toolbar.MinSize().Height, l.controls.MinSize().Height)
}
