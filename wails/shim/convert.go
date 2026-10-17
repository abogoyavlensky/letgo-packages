package shim

import (
	"fmt"
	"math"

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

// FromJSON raises a JSON-decoded Go value - what Wails hands over for a
// call's arguments and an event's data - to a let-go value.
//
// vm.ToLetGo would do most of it, but JSON has one number type and Go
// decodes it as float64, so an id of 3 would arrive as 3.0. A float with
// no fractional part that fits an int64 becomes an Int; anything else
// stays a Float. Objects become maps with string keys, arrays vectors, and
// null nil (which vm.ToLetGo cannot box at all).
func FromJSON(v any) (vm.Value, error) {
	switch t := v.(type) {
	case nil:
		return vm.NIL, nil
	case float64:
		// 2^63 is exact as a float64 while MaxInt64 is not (it rounds up
		// to 2^63), so the upper bound has to be exclusive.
		if t == math.Trunc(t) && t >= -(1<<63) && t < 1<<63 {
			return vm.Int(int64(t)), nil
		}
		return vm.Float(t), nil
	case []any:
		out := make([]vm.Value, len(t))
		for i, e := range t {
			lv, err := FromJSON(e)
			if err != nil {
				return nil, err
			}
			out[i] = lv
		}
		return vm.NewPersistentVector(out), nil
	case map[string]any:
		var m vm.Associative = vm.EmptyPersistentMap
		for k, e := range t {
			lv, err := FromJSON(e)
			if err != nil {
				return nil, err
			}
			m = m.Assoc(vm.String(k), lv)
		}
		return m.(vm.Value), nil
	}
	return vm.ToLetGo(v)
}
