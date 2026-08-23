// Package shim supplies the pieces of the database/sql wrapper that
// generation cannot produce, for any database/sql driver.
//
// lginterop boxes Go functions and dispatches methods on boxed values
// reflectively, which covers nearly all of database/sql: (.Next rows),
// (.Columns rows), (.Close rows), (.Begin db). The exceptions:
//
//   - Scan communicates results by writing through caller-allocated pointers,
//     and let-go values are immutable — (.Scan rows x) passes values, not
//     destinations. ScanRow allocates the destinations, calls Scan, and
//     returns the results as a value.
//
//   - Methods are not first-class values in let-go, so a variadic method
//     cannot be `apply`ed: (.Query db q p1 p2) works with literal arguments,
//     but a wrapper holding its parameters in a vector has no way to spread
//     them. Query and Exec take []any and spread it here.
//
//   - *sql.DB and *sql.Tx are unrelated types and database/sql declares no
//     common interface, so a veneer working identically inside and outside a
//     transaction needs one declared here: Connectable.
package shim

import (
	"database/sql"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// Connectable is the shape shared by *sql.DB and *sql.Tx. Both satisfy it,
// which is what lets the veneer's execute! take either.
type Connectable interface {
	Query(string, ...any) (*sql.Rows, error)
	Exec(string, ...any) (sql.Result, error)
}

// ScanRow reads the current row into a slice of values, sized from the
// column count. A NULL column comes back as a let-go nil, and drivers that
// return TEXT as []byte (sqlite) surface as ordinary strings, both courtesy
// of the boxing layer's dynamic-type handling for []any elements.
func ScanRow(rows *sql.Rows) ([]any, error) {
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
	return dest, nil
}

// Query runs a parameterized query, taking its arguments as a slice so a
// let-go vector of parameters can be passed straight through. A let-go nil
// parameter arrives as a nil element and binds as SQL NULL.
func Query(c Connectable, query string, args []any) (*sql.Rows, error) {
	return c.Query(query, args...)
}

// Exec runs a parameterized statement. Same reason as Query.
func Exec(c Connectable, query string, args []any) (sql.Result, error) {
	return c.Exec(query, args...)
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller — the same reason the -out-pkg output does. pkg/rt
// drains its installer queue during its own package init, and Go runs an
// imported package's init first, so anything queued from here would arrive
// after the drain and silently never run.
func init() {
	ns := vm.NewNamespace("sql.shim")
	ns.Def("ScanRow", vm.MustBox(ScanRow))
	ns.Def("Query", vm.MustBox(Query))
	ns.Def("Exec", vm.MustBox(Exec))
	rt.RegisterNS(ns)
}
