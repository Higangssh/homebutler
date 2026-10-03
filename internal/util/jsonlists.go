package util

import (
	"encoding/json"
	"reflect"
	"strings"
)

// EmptyLists returns v with every nil slice and map that would be written as
// null written as empty instead, at any depth.
//
// A list field without omitempty always appears, and an empty Go slice that
// was never allocated is nil, which encoding/json writes as null. null says the
// field is missing; [] says it has nothing in it, and only the second is true.
// alerts_history answered null on a machine with no history while the CLI
// printed [] for the same thing. MCP, the HTTP routes and --json all pass their
// answer through here, so the three cannot disagree again.
//
// It leaves alone what means "absent" on purpose: a field with omitempty, a
// nil pointer, and anything that marshals itself. The value passed in is not
// modified; the result is a copy where anything had to change.
func EmptyLists(v any) any {
	if v == nil {
		return nil
	}
	rv := emptyLists(reflect.ValueOf(v))
	if (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map) && rv.IsNil() {
		rv = emptyOf(rv.Type())
	}
	return rv.Interface()
}

var marshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

func emptyLists(v reflect.Value) reflect.Value {
	if v.Type().Implements(marshaler) {
		return v
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || v.Type().Elem().Implements(marshaler) {
			return v
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(emptyLists(v.Elem()))
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := emptyLists(v.Elem())
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			field := emptyLists(out.Field(i))
			if (field.Kind() == reflect.Slice || field.Kind() == reflect.Map) && field.IsNil() && !hasOpt(opts, "omitempty") {
				field = emptyOf(field.Type())
			}
			out.Field(i).Set(field)
		}
		return out
	case reflect.Slice:
		if v.IsNil() || !mayHoldLists(v.Type().Elem()) {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(emptyLists(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() || !mayHoldLists(v.Type().Elem()) {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			out.SetMapIndex(k, emptyLists(v.MapIndex(k)))
		}
		return out
	}
	return v
}

func emptyOf(t reflect.Type) reflect.Value {
	if t.Kind() == reflect.Map {
		return reflect.MakeMap(t)
	}
	return reflect.MakeSlice(t, 0, 0)
}

// mayHoldLists says whether walking into elements of t could change anything.
// A []string or map[string]int cannot, and is returned as it is.
func mayHoldLists(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct, reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		return true
	}
	return false
}

func hasOpt(opts, want string) bool {
	for opts != "" {
		var opt string
		opt, opts, _ = strings.Cut(opts, ",")
		if opt == want {
			return true
		}
	}
	return false
}
