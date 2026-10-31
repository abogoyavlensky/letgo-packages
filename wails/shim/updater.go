package shim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// In-app updates over Wails' own updater (app.Updater). Wails finds the
// release, downloads and checks it, swaps the bundle and relaunches; the
// app decides when to check and draws its own UI (Window: WindowNone).
//
// What this adds is a signer check. Wails' codesign pass only asks
// whether a bundle's signature is intact, and lets an unsigned one
// through. Here a staged .app must also satisfy the running app's
// designated requirement, the check Sparkle makes: an update has to come
// from whoever signed the installed copy.
//
// Every fn returns (vm.Value, error), never a bare error: a bare error
// comes back to let-go as a value, where (vm.Value, error) throws.

const (
	checkTimeout    = 20 * time.Second
	codesignTimeout = 90 * time.Second
)

// verified is the staged path that passed the signer check. Restart
// refuses anything else, so a download that failed the check can never be
// swapped in.
var (
	verifiedMu sync.Mutex
	verified   string
)

// requireDigest is set when the app names a checksum asset. Wails' GitHub
// provider skips verification when the sidecar is missing or has no line
// for the asset; with a checksum asset configured, that release is
// refused instead.
var requireDigest bool

// UpdaterInit configures app.Updater. opts: :current-version and
// :repository (required), :checksum-asset, :base-url (a stand-in for
// https://api.github.com). A second call is a no-op and the first config
// wins: application.New hands back the existing app, so an app that
// restarts its system in-process reaches this again.
func UpdaterInit(app *application.App, opts vm.Value) (vm.Value, error) {
	version, _ := optString(opts, "current-version")
	repo, _ := optString(opts, "repository")
	cfg := github.Config{Repository: repo}
	cfg.ChecksumAsset, _ = optString(opts, "checksum-asset")
	cfg.BaseURL, _ = optString(opts, "base-url")
	p, err := github.New(cfg)
	if err != nil {
		return vm.NIL, err
	}
	err = app.Updater.Init(updater.Config{
		CurrentVersion: version,
		Providers:      []updater.Provider{p},
		Window:         updater.WindowNone,
	})
	if errors.Is(err, updater.ErrAlreadyConfigured) {
		return vm.NIL, nil
	}
	if err == nil {
		requireDigest = cfg.ChecksumAsset != ""
	}
	return vm.NIL, err
}

// UpdaterCheck asks the provider for a newer release. Returns it as a map
// (:version :name :url :published-at :size), or nil when up to date.
func UpdaterCheck(app *application.App) (vm.Value, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	rel, err := app.Updater.Check(ctx)
	if err != nil || rel == nil {
		return vm.NIL, err
	}
	if requireDigest && (rel.Verification == nil || len(rel.Verification.Digest) == 0) {
		return vm.NIL, fmt.Errorf("release %s lists no checksum for %s", rel.Version, rel.Artifact.Filename)
	}
	url, _ := rel.Metadata["github.release.htmlURL"].(string)
	published := ""
	if !rel.PublishedAt.IsZero() {
		published = rel.PublishedAt.UTC().Format(time.RFC3339)
	}
	return FromJSON(map[string]any{
		"version":      rel.Version,
		"name":         rel.Name,
		"url":          url,
		"published-at": published,
		"size":         rel.Artifact.Size,
	})
}

// UpdaterDownload downloads the release the last check found, verifies
// its digest, unpacks it and runs the signer check on the staged bundle.
// It blocks; progress goes to the frontend as
// wails:updater:download-progress events. On darwin a bundle that fails
// the signer check is deleted and the error returned. Elsewhere there is
// no signer to check, and UpdaterCanInstall is false anyway.
func UpdaterDownload(app *application.App) (vm.Value, error) {
	verifiedMu.Lock()
	verified = ""
	verifiedMu.Unlock()
	if err := app.Updater.DownloadAndInstall(context.Background()); err != nil {
		return vm.NIL, err
	}
	staged := app.Updater.DownloadedPath()
	if runtime.GOOS == "darwin" {
		if err := checkSigner(staged); err != nil {
			_ = os.RemoveAll(filepath.Dir(staged))
			return vm.NIL, err
		}
	}
	verifiedMu.Lock()
	verified = staged
	verifiedMu.Unlock()
	return vm.NIL, nil
}

// UpdaterRestart hands the verified update to Wails' helper and quits;
// the helper swaps the bundle once this process has exited and starts the
// new one.
func UpdaterRestart(app *application.App) (vm.Value, error) {
	verifiedMu.Lock()
	ok := verified != "" && verified == app.Updater.DownloadedPath()
	verifiedMu.Unlock()
	if !ok {
		return vm.NIL, errors.New("no verified update to install")
	}
	return vm.NIL, app.Updater.Restart(context.Background())
}

// UpdaterCanInstall says whether this copy can replace itself: a macOS
// .app signed with an Apple-issued certificate (not ad hoc, so an update
// can be held to the same signer), in a folder this user can write to.
// Returns {:ok bool :reason string}.
func UpdaterCanInstall() (vm.Value, error) {
	reason := cannotInstall()
	return FromJSON(map[string]any{"ok": reason == "", "reason": reason})
}

func cannotInstall() string {
	if runtime.GOOS != "darwin" {
		return "updates install only on macOS"
	}
	bundle, err := runningBundle()
	if err != nil {
		return err.Error()
	}
	req, err := designatedRequirement(bundle)
	if err != nil {
		return err.Error()
	}
	if !strings.Contains(req, "anchor apple generic") {
		return "this copy is not signed with a Developer ID"
	}
	f, err := os.CreateTemp(filepath.Dir(bundle), ".update-check-*")
	if err != nil {
		return "cannot write to " + filepath.Dir(bundle)
	}
	f.Close()
	_ = os.Remove(f.Name())
	return ""
}

// runningBundle is the .app the running executable sits in.
func runningBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	for dir := filepath.Dir(exe); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir, nil
		}
	}
	return "", errors.New("not running from a .app bundle")
}

// designatedRequirement is the requirement text codesign reports for
// bundle, the part after "designated => ".
func designatedRequirement(bundle string) (string, error) {
	out, err := codesign("-d", "-r-", bundle)
	if err != nil {
		return "", fmt.Errorf("cannot read the app's signature: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if _, req, ok := strings.Cut(line, "designated => "); ok {
			return strings.TrimSpace(req), nil
		}
	}
	return "", errors.New("the app has no designated requirement")
}

// checkSigner fails unless staged satisfies the running app's designated
// requirement.
func checkSigner(staged string) error {
	bundle, err := runningBundle()
	if err != nil {
		return err
	}
	req, err := designatedRequirement(bundle)
	if err != nil {
		return err
	}
	// The leading = makes codesign read the requirement as text, not a path.
	if _, err := codesign("--verify", "--deep", "--strict", "-R", "="+req, staged); err != nil {
		return fmt.Errorf("the update is not signed by this app's developer: %w", err)
	}
	return nil
}

// codesign runs codesign and returns its stdout and stderr together; a
// non-zero exit is an error carrying that output.
func codesign(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), codesignTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codesign", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
