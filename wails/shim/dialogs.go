package shim

import (
	"fmt"

	"github.com/nooga/let-go/pkg/vm"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// OpenFile asks for one file to open and returns its path, or nil when
// the user cancels. opts: :title, :filters - a vector of
// [display-name pattern] pairs, pattern semicolon-separated ("*.json").
// Wails moves the dialog onto the main thread itself; call it from a
// handler or a menu click, never from the main thread while it blocks.
func OpenFile(app *application.App, opts vm.Value) (vm.Value, error) {
	filters, err := fileFilters(opts)
	if err != nil {
		return vm.NIL, err
	}
	title, _ := optString(opts, "title")
	path, err := app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		CanChooseFiles: true,
		Title:          title,
		Filters:        filters,
	}).PromptForSingleSelection()
	return pathOrNil(path, err)
}

// SaveFile asks where to save and returns the chosen path, or nil when
// the user cancels. opts: :title, :filename (the suggested name),
// :filters as for OpenFile.
func SaveFile(app *application.App, opts vm.Value) (vm.Value, error) {
	filters, err := fileFilters(opts)
	if err != nil {
		return vm.NIL, err
	}
	title, _ := optString(opts, "title")
	filename, _ := optString(opts, "filename")
	path, err := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		CanCreateDirectories: true,
		Title:                title,
		Filename:             filename,
		Filters:              filters,
	}).PromptForSingleSelection()
	return pathOrNil(path, err)
}

func pathOrNil(path string, err error) (vm.Value, error) {
	if err != nil {
		return vm.NIL, err
	}
	if path == "" {
		return vm.NIL, nil
	}
	return vm.String(path), nil
}

func fileFilters(opts vm.Value) ([]application.FileFilter, error) {
	raw, ok := ToGo(opt(opts, "filters")).([]any)
	if !ok {
		return nil, nil
	}
	out := make([]application.FileFilter, 0, len(raw))
	for i, f := range raw {
		pair, ok := f.([]any)
		if !ok || len(pair) != 2 {
			return nil, fmt.Errorf(":filters %d must be a [display-name pattern] pair", i)
		}
		name, ok1 := pair[0].(string)
		pattern, ok2 := pair[1].(string)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf(":filters %d must be a pair of strings", i)
		}
		out = append(out, application.FileFilter{DisplayName: name, Pattern: pattern})
	}
	return out, nil
}

// Message shows a message dialog. kind is "info", "warning" or "error".
func Message(app *application.App, kind, title, body string) (bool, error) {
	var d *application.MessageDialog
	switch kind {
	case "info":
		d = app.Dialog.Info()
	case "warning":
		d = app.Dialog.Warning()
	case "error":
		d = app.Dialog.Error()
	default:
		return false, fmt.Errorf("message kind must be :info, :warning or :error, got %q", kind)
	}
	d.SetTitle(title).SetMessage(body).Show()
	return true, nil
}
