//go:build (linux || freebsd || openbsd || netbsd) && !wayland

package gui

import (
	"image"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/randr"
	"github.com/BurntSushi/xgb/xinerama"
	"github.com/BurntSushi/xgb/xproto"
)

func nativeWindowWorkArea(context any, scale float32) fyne.Size {
	window, ok := context.(driver.X11WindowContext)
	if !ok {
		return fyne.Size{}
	}
	connection, err := xgb.NewConn()
	if err != nil {
		return fyne.Size{}
	}
	defer connection.Close()
	screen := xproto.Setup(connection).DefaultScreen(connection)
	area := image.Rect(0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels))
	point := area.Min
	// The primary output need not contain (0,0) in a multi-monitor desktop.
	// Choose it explicitly for a window that has not been created yet.
	if window.WindowHandle == 0 {
		systemScale := float32(1)
		if randr.Init(connection) == nil {
			if primary, err := randr.GetOutputPrimary(connection, screen.Root).Reply(); err == nil && primary.Output != 0 {
				if output, err := randr.GetOutputInfo(connection, primary.Output, xproto.TimeCurrentTime).Reply(); err == nil && output.Crtc != 0 {
					if crtc, err := randr.GetCrtcInfo(connection, output.Crtc, xproto.TimeCurrentTime).Reply(); err == nil {
						point = image.Pt(int(crtc.X), int(crtc.Y))
						if output.MmWidth > 0 && !(output.MmWidth == 60 && output.MmHeight == 60) {
							dpi := float32(crtc.Width) * 25.4 / float32(output.MmWidth)
							if dpi >= 10 && dpi <= 1000 {
								systemScale = max(1, dpi/120)
							}
						}
					}
				}
			}
		}
		disable := os.Getenv("FYNE_DISABLE_DPI_DETECTION")
		if disable == "1" || strings.EqualFold(disable, "true") || strings.EqualFold(disable, "t") {
			systemScale = 1
		}
		scale = initialWindowScale(systemScale)
	}
	if window.WindowHandle != 0 {
		if reply, err := xproto.TranslateCoordinates(connection, xproto.Window(window.WindowHandle), screen.Root, 0, 0).Reply(); err == nil {
			point = image.Pt(int(reply.DstX), int(reply.DstY))
		}
	}
	if xinerama.Init(connection) == nil {
		if reply, err := xinerama.QueryScreens(connection).Reply(); err == nil {
			for _, monitor := range reply.ScreenInfo {
				bounds := image.Rect(int(monitor.XOrg), int(monitor.YOrg), int(monitor.XOrg)+int(monitor.Width), int(monitor.YOrg)+int(monitor.Height))
				if point.In(bounds) {
					area = bounds
					break
				}
			}
		}
	}
	property := func(name string, offset, length uint32) []byte {
		atom, err := xproto.InternAtom(connection, true, uint16(len(name)), name).Reply()
		if err != nil || atom.Atom == 0 {
			return nil
		}
		value, err := xproto.GetProperty(connection, false, screen.Root, atom.Atom, xproto.AtomCardinal, offset, length).Reply()
		if err != nil || value.Format != 32 {
			return nil
		}
		return value.Value
	}
	desktop := uint32(0)
	if value := property("_NET_CURRENT_DESKTOP", 0, 1); len(value) >= 4 {
		desktop = xgb.Get32(value)
	}
	if desktop < 1024 {
		if value := property("_NET_WORKAREA", desktop*4, 4); len(value) >= 16 {
			x, y := int(int32(xgb.Get32(value))), int(int32(xgb.Get32(value[4:])))
			bounds := image.Rect(x, y, x+int(xgb.Get32(value[8:])), y+int(xgb.Get32(value[12:])))
			if intersection := area.Intersect(bounds); !intersection.Empty() {
				area = intersection
			}
		}
	}
	return fyne.NewSize(float32(area.Dx())/max(.1, scale), float32(area.Dy())/max(.1, scale))
}
