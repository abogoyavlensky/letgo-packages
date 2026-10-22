//go:build !production

package shim

import "os"

// DevServerURL is the frontend dev server the app's assets come from, or
// "" when there is none: FRONTEND_DEVSERVER_URL, the variable Wails' own
// dev tooling sets. Read on every call, so it can change while the app
// runs (the tests rely on that).
func DevServerURL() string { return os.Getenv("FRONTEND_DEVSERVER_URL") }
