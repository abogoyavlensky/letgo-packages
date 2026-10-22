package shim

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// resourceAssets serves the frontend from let-go's resources: URL path /x
// is the resource <prefix>/x, and / is <prefix>/index.html.
//
// This is the one place the frontend comes from in both modes. Under
// `lgx run` the installed provider reads the project's :resource-paths
// from disk on every request, so an edited file shows up on the next load.
// In a binary built by `lgx build` it reads the archive embedded in the
// executable, so the app does not depend on its working directory - a
// macOS .app started from Finder runs with cwd /, which is where a
// directory-relative asset dir goes blank.
type resourceAssets struct {
	prefix string
}

func (h resourceAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "" || strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	name, ok := rt.NormalizeResourceName(h.prefix + "/" + strings.TrimLeft(p, "/"))
	// NormalizeResourceName rejects escapes past the resource root; this
	// also rejects escapes past the prefix, which stay inside the root.
	if !ok || !strings.HasPrefix(name, h.prefix+"/") {
		http.NotFound(w, r)
		return
	}
	provider := rt.GetResourceProvider()
	if provider == nil {
		http.NotFound(w, r)
		return
	}
	rc, ok := provider.Open(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// No caching: under `lgx run` the files change underneath the app.
	w.Header().Set("Cache-Control", "no-store")
	// Commit the status before the body. An empty resource makes io.Copy
	// return without a Write, and Wails' native (macOS, Linux) response
	// writers answer 501 when no header was ever written.
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func newResourceAssets(prefix string) http.Handler {
	return resourceAssets{prefix: strings.Trim(prefix, "/")}
}

// devAware serves the frontend from the dev server while DevServerURL is
// set, and from inner otherwise. Wails' own proxy
// (application.AssetFileServerFS) only consults FRONTEND_DEVSERVER_URL
// when it is the asset handler, which :assets and :assets-dir replace; this
// puts it back in front of them. The page keeps Wails' origin, so the
// bridge (/wails/*, answered by Wails before any asset handler) still
// works, and Vite's HMR client talks to Vite directly.
//
// The URL is read per request rather than once at New: Wails allows one
// app per process, so this is the only way a test can flip it.
type devAware struct {
	inner http.Handler

	mu    sync.Mutex
	url   string       // the URL proxy was built for
	proxy http.Handler // Wails reads the variable when building it
}

func (h *devAware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := DevServerURL()
	if url == "" {
		h.inner.ServeHTTP(w, r)
		return
	}
	h.proxyFor(url).ServeHTTP(w, r)
}

func (h *devAware) proxyFor(url string) http.Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.proxy == nil || h.url != url {
		h.proxy = application.AssetFileServerFS(nil)
		h.url = url
	}
	return h.proxy
}

func withDevServer(inner http.Handler) http.Handler {
	return &devAware{inner: inner}
}
