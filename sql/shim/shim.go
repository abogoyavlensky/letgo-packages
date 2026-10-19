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
//   - *sql.DB, *sql.Tx and *sql.Conn are unrelated types and database/sql
//     declares no common interface, so a veneer working identically on a
//     pool, inside a transaction and on a dedicated session needs one
//     declared here: Connectable.
//
//   - let-go cannot build a context.Context, and cancellation needs one.
//     Ctx carries a cancellable context a caller can hold and cancel, and
//     QueryCtx, ExecCtx and Conn take it (or context.Background) on the
//     caller's behalf.
package shim

import (
	"context"
	"database/sql"

	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// Connectable is the shape shared by *sql.DB, *sql.Tx and *sql.Conn. All
// three have the ...Context methods (*sql.Conn has nothing else), which is
// what lets the veneer's execute! take any of them.
type Connectable interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Ctx is a cancellable context for one query. Create it with NewCtx before
// running the query, so another thread can (.Cancel ctx) before the first
// row arrives; cancel it once the query is done to release it.
type Ctx struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewCtx returns a fresh, uncancelled Ctx.
func NewCtx() *Ctx {
	ctx, cancel := context.WithCancel(context.Background())
	return &Ctx{ctx: ctx, cancel: cancel}
}

// Cancel cancels the context. A query still executing is interrupted by
// the driver; rows already returned are closed by database/sql. Calling it
// again is a no-op.
func (c *Ctx) Cancel() { c.cancel() }

// Canceled reports whether Cancel has been called.
func (c *Ctx) Canceled() bool { return c.ctx.Err() != nil }

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
	return c.QueryContext(context.Background(), query, args...)
}

// Exec runs a parameterized statement. Same reason as Query.
func Exec(c Connectable, query string, args []any) (sql.Result, error) {
	return c.ExecContext(context.Background(), query, args...)
}

// QueryCtx is Query under a cancellable Ctx.
func QueryCtx(c Connectable, ctx *Ctx, query string, args []any) (*sql.Rows, error) {
	return c.QueryContext(ctx.ctx, query, args...)
}

// ExecCtx is Exec under a cancellable Ctx.
func ExecCtx(c Connectable, ctx *Ctx, query string, args []any) (sql.Result, error) {
	return c.ExecContext(ctx.ctx, query, args...)
}

// Conn takes a dedicated connection - a session - from the pool. Session
// state (an open transaction, temp tables, SET) stays on it across calls,
// which the pool does not guarantee. Close it to return it to the pool.
func Conn(db *sql.DB) (*sql.Conn, error) {
	return db.Conn(context.Background())
}

// ColumnTypeNames returns each column's database type name ("INTEGER",
// "TEXT", "INT8", ...), in column order; "" where the driver does not
// know it.
func ColumnTypeNames(rows *sql.Rows) ([]string, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.DatabaseTypeName()
	}
	return names, nil
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
	ns.Def("NewCtx", vm.MustBox(NewCtx))
	ns.Def("QueryCtx", vm.MustBox(QueryCtx))
	ns.Def("ExecCtx", vm.MustBox(ExecCtx))
	ns.Def("Conn", vm.MustBox(Conn))
	ns.Def("ColumnTypeNames", vm.MustBox(ColumnTypeNames))
	rt.RegisterNS(ns)
}
