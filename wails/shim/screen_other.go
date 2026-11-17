//go:build !darwin

package shim

import "github.com/wailsapp/wails/v3/pkg/application"

// windowWorkArea is the work area of the window's screen in the units of a
// window's bounds. Off macOS, GetScreen already answers those (GTK4's
// monitor geometry is in logical pixels); see screen_darwin.go for macOS.
// On Linux Wails fills WorkArea with the whole monitor, panels included:
// GTK4 has no portable work-area call (gdk_x11_monitor_get_workarea is
// X11-only), so this passes it on as it is and the docs say so.
func windowWorkArea(s *application.Screen) application.Rect {
	return s.WorkArea
}
