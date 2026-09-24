// Package shim reads DuckDB rows into plain let-go values.
//
// sql.core reads rows through sql.shim/ScanRow, which scans into []any and
// leaves each value to let-go's boxing layer. That is enough for sqlite and
// postgres, but DuckDB's driver hands back Go types the boxing layer can only
// wrap opaquely, and analytics queries hit them constantly:
//
//   - sum() over INTEGER/BIGINT widens to HUGEINT, which arrives as *big.Int,
//     so the most common aggregate would print as <go.*big.Int 3>.
//   - UBIGINT arrives as uint64, and boxing converts it with Int(v.Uint()),
//     wrapping anything past int64 to a negative number.
//   - DATE/TIME/TIMESTAMP arrive as time.Time, DECIMAL as duckdb.Decimal,
//     INTERVAL as duckdb.Interval, MAP as duckdb.OrderedMap: all opaque.
//   - UUID arrives as 16 raw bytes, which box as an unreadable string.
//   - STRUCT arrives as map[string]any, which boxes with string keys.
//
// ScanRow converts each value into a let-go value itself and returns the row
// as a let-go vector. sql.core reaches it through the connectable's
// :sql/scan-row key, which duckdb.core/open sets. Dates and times become
// ISO-8601 strings shaped by the column's DuckDB type; numbers too large for
// an int64 become decimal strings rather than lossy floats.
package shim

import (
	"database/sql"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/nooga/let-go/pkg/rt"
	"github.com/nooga/let-go/pkg/vm"
)

// ScanRow reads the current row and returns its values, converted, as a
// let-go vector in column order.
func ScanRow(rows *sql.Rows) (vm.Value, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	dest := make([]any, len(types))
	ptrs := make([]any, len(types))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := make([]vm.Value, len(dest))
	for i, v := range dest {
		out[i] = convert(v, types[i].DatabaseTypeName())
	}
	return vm.NewArrayVector(out), nil
}

// convert turns one driver value into a let-go value. colType is the DuckDB
// type name of a top-level column, and "" for a value nested in a LIST,
// STRUCT or MAP, which carries no type of its own.
func convert(v any, colType string) vm.Value {
	switch x := v.(type) {
	case nil:
		return vm.NIL
	case uint64:
		if x > math.MaxInt64 {
			return vm.String(new(big.Int).SetUint64(x).String())
		}
		return vm.Int(int64(x))
	case uint:
		return convert(uint64(x), colType)
	case *big.Int:
		if x.IsInt64() {
			return vm.Int(x.Int64())
		}
		return vm.String(x.String())
	case duckdb.Decimal:
		return vm.String(x.String())
	case time.Time:
		return vm.String(formatTime(x, colType))
	case []byte:
		if colType == "UUID" && len(x) == 16 {
			var u duckdb.UUID
			copy(u[:], x)
			return vm.String(u.String())
		}
	case duckdb.Interval:
		return vm.NewArrayMap([]vm.Value{
			vm.Keyword("months"), vm.Int(int64(x.Months)),
			vm.Keyword("days"), vm.Int(int64(x.Days)),
			vm.Keyword("micros"), vm.Int(x.Micros),
		})
	case []any:
		out := make([]vm.Value, len(x))
		for i, e := range x {
			out[i] = convert(e, "")
		}
		return vm.NewArrayVector(out)
	case map[string]any:
		// Go map order is random; sort for a deterministic map.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kvs := make([]vm.Value, 0, 2*len(x))
		for _, k := range keys {
			kvs = append(kvs, vm.Keyword(k), convert(x[k], ""))
		}
		return vm.NewArrayMap(kvs)
	case duckdb.OrderedMap:
		ks, vs := x.Keys(), x.Values()
		kvs := make([]vm.Value, 0, 2*len(ks))
		for i := range ks {
			kvs = append(kvs, convert(ks[i], ""), convert(vs[i], ""))
		}
		return vm.NewArrayMap(kvs)
	}
	// Everything else - bool, string, the other int and float kinds, BLOB
	// bytes, and the types with no natural let-go shape (UNION, BIT) - is
	// what the boxing layer already produces.
	b, err := vm.BoxValue(reflect.ValueOf(v))
	if err != nil {
		return vm.NIL
	}
	return b
}

// formatTime renders a time.Time as ISO-8601 in the shape of its column's
// DuckDB type. Trailing fractional zeros are trimmed by the .999 layouts.
func formatTime(t time.Time, colType string) string {
	switch {
	case colType == "DATE":
		return t.Format("2006-01-02")
	case colType == "TIME":
		return t.Format("15:04:05.999999")
	case colType == "TIMETZ":
		return t.Format("15:04:05.999999Z07:00")
	case colType == "TIMESTAMPTZ":
		return t.UTC().Format(time.RFC3339Nano)
	case strings.HasPrefix(colType, "TIMESTAMP"):
		// TIMESTAMP, TIMESTAMP_S/_MS/_NS: naive, so no zone suffix.
		return t.Format("2006-01-02T15:04:05.999999999")
	default:
		return t.UTC().Format(time.RFC3339Nano)
	}
}

// init registers the namespace directly rather than through
// rt.RegisterInstaller: pkg/rt drains its installer queue during its own
// package init, and Go runs an imported package's init first, so anything
// queued from here would arrive after the drain and silently never run.
func init() {
	ns := vm.NewNamespace("duckdb.shim")
	ns.Def("ScanRow", vm.MustBox(ScanRow))
	rt.RegisterNS(ns)
}
