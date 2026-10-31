package modelgpu

import (
	"emanuelelongo/proview/internal/model3d"
	"fmt"
	"math"
)

func validate(m *model3d.Mesh, c model3d.Camera, w, h int) error {
	if m == nil || m.TriangleCount() == 0 || w < 1 || h < 1 || w > 2400 || h > 2400 || int64(w)*int64(h) > 4_000_000 {
		return fmt.Errorf("invalid model viewport")
	}
	for _, v := range []float64{c.Yaw, c.Pitch, c.Zoom, c.PanX, c.PanY} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("invalid camera")
		}
	}
	return nil
}
