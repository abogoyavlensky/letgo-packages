package shim

import (
	"fmt"
	"os"

	"github.com/nooga/let-go/pkg/vm"
)

// callback invokes a let-go fn from a Wails goroutine - a menu click, an
// event, the shutdown hook. Nothing on the Go side waits for a result, so
// an error has nowhere to go but stderr; it must never panic, which would
// take the whole app down with it.
func callback(where string, fn vm.Fn, args ...vm.Value) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "wails: %s: panic: %v\n", where, r)
		}
	}()
	if _, err := fn.Invoke(args); err != nil {
		fmt.Fprintf(os.Stderr, "wails: %s: %s\n", where, vm.FormatError(err))
	}
}
