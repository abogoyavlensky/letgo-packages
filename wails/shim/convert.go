package shim

import (
	"fmt"

	"github.com/nooga/let-go/pkg/vm"
)

// ToGo lowers a let-go value to something encoding/json can marshal.
//
// Unbox alone is not enough: a map and a vector both unbox to themselves.
// A let-go map is a *vm.PersistentMap whose Seq yields vm.MapEntry, so a
// type switch on vm.Map alone silently turns every map into a list of
// pairs - JSON arrays where the frontend expects objects.
func ToGo(v vm.Value) any {
	if v == nil || v == vm.NIL {
		return nil
	}
	switch t := v.(type) {
	case vm.Keyword:
		return string(t)
	case vm.Symbol:
		return string(t)
	case vm.String:
		return string(t)
	}
	if s, ok := v.(vm.Sequable); ok {
		sq := s.Seq()
		if sq != nil {
			if _, isEntry := sq.First().(vm.MapEntry); isEntry {
				out := map[string]any{}
				for ; sq != nil; sq = sq.Next() {
					e := sq.First().(vm.MapEntry)
					out[keyString(e.Key)] = ToGo(e.Value)
				}
				return out
			}
		}
		out := []any{}
		for ; sq != nil; sq = sq.Next() {
			out = append(out, ToGo(sq.First()))
		}
		return out
	}
	return v.Unbox()
}

// keyString renders a map key as the JSON object key. Keywords lose their
// leading colon, which is what a JavaScript caller expects - and what lets
// every options map here take :title and "title" alike.
func keyString(k vm.Value) string {
	switch t := k.(type) {
	case vm.Keyword:
		return string(t)
	case vm.String:
		return string(t)
	case vm.Symbol:
		return string(t)
	}
	return fmt.Sprintf("%v", k.Unbox())
}

// opt reads key from a let-go options map, as a keyword or a string key.
// It returns the raw let-go value, so a fn stays a vm.Fn (ToGo would
// unbox it). nil when absent or when m is not a map.
func opt(m vm.Value, key string) vm.Value {
	l, ok := m.(vm.Lookup)
	if !ok || m == vm.NIL {
		return nil
	}
	for _, k := range []vm.Value{vm.Keyword(key), vm.String(key)} {
		if v := l.ValueAt(k); v != nil && v != vm.NIL {
			return v
		}
	}
	return nil
}

func optString(m vm.Value, key string) (string, bool) {
	s, ok := ToGo(opt(m, key)).(string)
	return s, ok
}

func optInt(m vm.Value, key string) (int, bool) {
	return asInt(ToGo(opt(m, key)))
}

func optBool(m vm.Value, key string) bool {
	b, _ := ToGo(opt(m, key)).(bool)
	return b
}

// optFn reads a callback. A var counts: *vm.Var implements vm.Fn and
// re-reads its root on every call, which is what makes a callback hot.
func optFn(m vm.Value, key string) (vm.Fn, error) {
	v := opt(m, key)
	if v == nil {
		return nil, nil
	}
	fn, ok := v.(vm.Fn)
	if !ok {
		return nil, fmt.Errorf("%s must be a fn or a var, got %s", key, v.Type().Name())
	}
	return fn, nil
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
