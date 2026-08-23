package config

import "reflect"

// secretTag marks a field that holds a plaintext credential.
//
// Fields carrying this tag are what hasSecrets looks for, and they are also
// the fields that must never be serialized: tag them `json:"-"` as well, so a
// leak is structurally impossible rather than merely absent from today's call
// sites.
const secretTag = "secret"

// hasSecrets reports whether cfg holds a plaintext credential.
//
// It walks the config rather than checking a hand-kept list of fields. The
// list was the bug: it knew only about servers[].password, so a config holding
// nothing but a Telegram bot token was never permission-checked at all, and
// every credential-bearing section added since would have had to be remembered
// in a function far away from the field it was about. A field now declares
// what it is, next to itself.
func hasSecrets(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return containsSecret(reflect.ValueOf(cfg))
}

func containsSecret(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return false
		}
		return containsSecret(v.Elem())

	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if containsSecret(v.Index(i)) {
				return true
			}
		}

	case reflect.Map:
		for _, k := range v.MapKeys() {
			if containsSecret(v.MapIndex(k)) {
				return true
			}
		}

	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			// A tagged field counts only when it actually holds something.
			// An empty password is not a secret to protect.
			if f.Tag.Get(secretTag) == "true" {
				if fv := v.Field(i); fv.Kind() == reflect.String && fv.String() != "" {
					return true
				}
				continue
			}
			if containsSecret(v.Field(i)) {
				return true
			}
		}
	}

	return false
}
