package document

import (
	"emanuelelongo/proview/internal/gifplay"
	img "emanuelelongo/proview/pkg/images"
)

// Animation is available only for an unmodified image. Editing and export use
// the immutable first frame, independently of playback state.
func (d *Document) Animation() *gifplay.Animation {
	if d == nil || d.crop != nil || d.imageTransform != (img.Transform{}) || len(d.signatures) != 0 || len(d.redactions) != 0 {
		return nil
	}
	return d.source.ensureAnimation()
}

// ensureAnimation decodes an evicted GIF animation again. It is independent of
// source.mu so a UI playback tick never waits behind a native page render.
func (s *Source) ensureAnimation() *gifplay.Animation {
	if s == nil || s.format != FormatGIF {
		return nil
	}
	s.animationMu.Lock()
	defer s.animationMu.Unlock()
	if s.animation == nil {
		if animation, err := gifplay.Decode(s.original, gifplay.MaxBytes); err == nil && animation.Animated() {
			s.animation = animation
			s.animationBytes.Store(animation.Bytes)
		}
	}
	return s.animation
}

func (s *Source) trimAnimation() {
	if s == nil {
		return
	}
	s.animationMu.Lock()
	s.animation = nil
	s.animationBytes.Store(0)
	s.animationMu.Unlock()
}
