// Package shim supplies the one piece of the database/sql wrapper that
// generation cannot produce.
//
// lginterop boxes Go functions and dispatches methods on boxed values
// reflectively, which covers nearly all of database/sql: (.Query db q & args),
// (.Next rows), (.Columns rows), (.Close rows). The exception is Scan:
//
//	func (rs *Rows) Scan(dest ...any) error
//
// Scan communicates results by writing through caller-allocated pointers, and
// let-go values are immutable — (.Scan rows x) passes values, not
// destinations. ScanRow allocates the destinations, calls Scan, and returns
// the results as a value.
//
// Query and Exec bridge a second gap. Methods are not first-class values in
// let-go, so a variadic method cannot be `apply`ed: (.Query db q p1 p2) works
// with literal arguments, but a wrapper holding its parameters in a vector has
// no way to spread them. Taking []any and spreading it here does.
package shim

import (
	"database/sql"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// ScanRow reads the current row into a slice of values, sized from the column
// count.
//
// The result is []vm.Value, not []any, and that matters. vm.BoxValue walks a
// slice by its *static* element type: for []any every element is kind
// Interface, misses the string/int fast paths, and arrives in let-go as an
// opaque vm.Boxed that prints as <go.string Ada> and compares equal to
// nothing. Converting each element here — where reflect sees its dynamic type
// — yields ordinary let-go strings, ints, and floats instead.
func ScanRow(rows *sql.Rows) ([]vm.Value, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	dest := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := make([]vm.Value, len(dest))
	for i, d := range dest {
		v, err := toValue(d)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// toValue converts one scanned column. The driver hands back the small set
// database/sql documents — nil, int64, float64, bool, []byte, string,
// time.Time — and the first two cases are the ones reflect alone gets wrong:
// a NULL is an untyped nil that reflect cannot box at all, and sqlite returns
// TEXT as []byte, which would otherwise surface as a byte array.
func toValue(d any) (vm.Value, error) {
	switch v := d.(type) {
	case nil:
		return vm.NIL, nil
	case []byte:
		return vm.ToLetGo(string(v))
	default:
		return vm.ToLetGo(d)
	}
}

// Query runs a parameterized query, taking its arguments as a slice so a
// let-go vector of parameters can be passed straight through.
//
// []vm.Value rather than []any for the mirror of ScanRow's reason: let-go
// converts a vector to []any element by element and gives up when an element
// has no Go counterpart, handing the whole slice over as []vm.Value instead —
// so a single nil parameter turns every call into a reflect type error.
// Unboxing here is total.
func Query(db *sql.DB, query string, args []vm.Value) (*sql.Rows, error) {
	return db.Query(query, unbox(args)...)
}

// Exec runs a parameterized statement. Same reason as Query.
func Exec(db *sql.DB, query string, args []vm.Value) (sql.Result, error) {
	return db.Exec(query, unbox(args)...)
}

// unbox turns let-go parameter values into the Go values database/sql binds.
// A let-go nil is SQL NULL.
func unbox(vs []vm.Value) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		if v == nil || v == vm.NIL {
			continue
		}
		out[i] = v.Unbox()
	}
	return out
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller — the same reason the -out-pkg output does. pkg/rt
// drains its installer queue during its own package init, and Go runs an
// imported package's init first, so anything queued from here would arrive
// after the drain and silently never run.
func init() {
	ns := vm.NewNamespace("sqlite.shim")
	ns.Def("ScanRow", vm.MustBox(ScanRow))
	ns.Def("Query", vm.MustBox(Query))
	ns.Def("Exec", vm.MustBox(Exec))
	rt.RegisterNS(ns)
}
