package contract

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/doctor"
	"github.com/Higangssh/homebutler/internal/mcp"
	"github.com/Higangssh/homebutler/internal/report"
	"github.com/Higangssh/homebutler/internal/system"
	"github.com/Higangssh/homebutler/internal/util"
)

// No list field without omitempty reaches a caller as null, on any path and at
// any depth: MCP, the HTTP routes and --json all pass their answer through
// util.EmptyLists. This holds every answer type the golden records to that,
// so a type added later is checked without anybody remembering to.
//
// A zero value only has nil lists at its top level, so each type is filled
// first: every list gets one element, three levels deep, and the lists below
// that are left nil for EmptyLists to find.
func TestNoAnswerHasANullList(t *testing.T) {
	types := map[string]reflect.Type{
		"report":        reflect.TypeOf(report.Report{}),
		"doctor":        reflect.TypeOf(doctor.Result{}),
		"system_status": reflect.TypeOf(system.StatusDoc{}),
	}
	for name, answer := range mcp.ToolOutputs {
		types[name] = reflect.TypeOf(answer)
	}
	for name, typ := range types {
		// The zero value is the empty answer — no history, no containers —
		// and the filled one reaches the lists inside the lists.
		for _, value := range []reflect.Value{reflect.Zero(typ), filled(typ, 0)} {
			v := util.EmptyLists(value.Interface())
			for _, path := range nilLists(reflect.ValueOf(v), name) {
				t.Errorf("%s would be null", path)
			}
		}
	}
}

func filled(t reflect.Type, depth int) reflect.Value {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.Struct:
		if t.PkgPath() == "time" {
			return v
		}
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && v.Field(i).CanSet() {
				v.Field(i).Set(filled(t.Field(i).Type, depth+1))
			}
		}
	case reflect.Slice:
		if depth < 3 {
			s := reflect.MakeSlice(t, 1, 1)
			s.Index(0).Set(filled(t.Elem(), depth+1))
			return s
		}
	case reflect.Pointer:
		if depth < 3 {
			p := reflect.New(t.Elem())
			p.Elem().Set(filled(t.Elem(), depth+1))
			return p
		}
	}
	return v
}

func nilLists(v reflect.Value, at string) []string {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return nilLists(v.Elem(), at)
	case reflect.Slice:
		if v.IsNil() {
			return []string{at}
		}
		var out []string
		for i := 0; i < v.Len(); i++ {
			out = append(out, nilLists(v.Index(i), at+"[]")...)
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return []string{at}
		}
	case reflect.Struct:
		var out []string
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" && !f.Anonymous {
				name = f.Name
			}
			path := at + "." + name
			if name == "" {
				path = at // an embedded struct's fields are the parent's keys
			}
			field := v.Field(i)
			if (field.Kind() == reflect.Slice || field.Kind() == reflect.Map) && field.IsNil() {
				if !strings.Contains(opts, "omitempty") {
					out = append(out, path)
				}
				continue
			}
			out = append(out, nilLists(field, path)...)
		}
		return out
	}
	return nil
}
