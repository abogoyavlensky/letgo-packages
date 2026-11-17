//go:build darwin

package shim

import (
	"math"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// windowWorkArea is the work area of the window's screen in points, the
// units of a window's bounds. On macOS, WebviewWindow.GetScreen hands back
// cScreenToScreen's values, which are multiplied by the backing scale
// factor so that the screen manager's DPI pass can divide them again; that
// pass never runs for GetScreen. This undoes the multiplication, or a
// Retina display would report a work area twice its size.
func windowWorkArea(s *application.Screen) application.Rect {
	sf := float64(s.ScaleFactor)
	if sf <= 0 {
		return s.WorkArea
	}
	in := func(n int) int { return int(math.Round(float64(n) / sf)) }
	return application.Rect{
		X: in(s.WorkArea.X), Y: in(s.WorkArea.Y),
		Width: in(s.WorkArea.Width), Height: in(s.WorkArea.Height),
	}
}
