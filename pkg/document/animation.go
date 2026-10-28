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
	return d.source.animation
}
