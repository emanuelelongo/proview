package gui

import (
	"emanuelelongo/proview/internal/gifplay"
	"emanuelelongo/proview/pkg/document"
	"time"

	"fyne.io/fyne/v2"
)

// One native animation clock per visible document. Decoded
// sources and printing remain immutable; only canvas image references change.
type gifPlayback struct {
	clock  *fyne.Animation
	doc    *document.Document
	view   *markdownView
	player *gifplay.Player
	paused bool
}

type markdownGIFGroup struct {
	player   *gifplay.Player
	segments []*markdownImageSegment
}

func (g *Gui) stopGIFPlayback() {
	if p := g.gifPlayback; p != nil {
		p.clock.Stop()
		g.gifPlayback = nil
	}
}

func (g *Gui) ensureGIFPlayback() {
	if g.closed {
		return
	}
	if p := g.gifPlayback; p != nil {
		if p.doc != nil && g.document == p.doc && g.document.Animation() == nil {
			g.pauseGIFImage()
			g.stopGIFPlayback()
		}
		return
	}
	p := &gifPlayback{}
	if v := g.markdownView; v != nil && len(v.animatedImages) > 0 {
		p.view = v
	} else if g.document != nil && g.document.Animation() != nil {
		p.doc = g.document
		p.player = g.document.Animation().NewPlayer()
	} else {
		return
	}
	g.gifPlayback = p
	p.clock = fyne.NewAnimation(time.Second, func(float32) {
		if !g.closed && g.gifPlayback == p {
			g.tickGIFPlayback(p, time.Now())
		}
	})
	p.clock.RepeatCount = fyne.AnimationRepeatForever
	p.clock.Start()
}

func (g *Gui) pauseGIFImage() {
	p := g.gifPlayback
	if p == nil || p.player == nil || p.doc != g.document || p.paused {
		return
	}
	p.paused = true
	p.player.Pause()
	if base, err := g.document.RenderBasePage(0); err == nil {
		g.image.Image = base
		g.image.Refresh()
	}
}

func (g *Gui) tickGIFPlayback(p *gifPlayback, now time.Time) {
	if p.player != nil {
		if g.document != p.doc {
			return
		}
		if g.working || g.cropOverlay.active || g.annotationTool != toolSelect || g.document.Animation() == nil || g.hasSelectedSignature || g.textSelection != nil {
			g.pauseGIFImage()
			return
		}
		if p.paused {
			p.paused = false
			g.image.Image = p.player.Image()
			g.image.Refresh()
		}
		if p.player.Advance(now) {
			g.image.Image = p.player.Image()
			g.image.Refresh()
		}
		return
	}
	v := p.view
	if g.markdownView != v || v.scroll == nil {
		return
	}
	for _, group := range v.gifGroups {
		preview := v.imagePreview
		previewing := preview != nil && preview.animation == group.player
		visible := false
		for _, segment := range group.segments {
			if block := segment.block; block != nil && block.positioned {
				y := block.pagePosition.Y - v.scroll.Offset.Y
				visible = y+block.Size().Height > 0 && y < v.scroll.Size().Height
			}
			if visible {
				break
			}
		}
		if !previewing && (!visible || preview != nil) {
			group.player.Pause()
			continue
		}
		if !group.player.Advance(now) {
			continue
		}
		picture := group.player.Image()
		for _, segment := range group.segments {
			segment.image = picture
			if segment.visual != nil {
				segment.visual.Image = picture
				segment.visual.Refresh()
			}
		}
		if previewing {
			preview.picture.Image = picture
			preview.picture.Refresh()
		}
	}
}
