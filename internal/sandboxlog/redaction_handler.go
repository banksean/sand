package sandboxlog

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
)

const redactedValue = "<redacted>"

var (
	envFlagPattern     = regexp.MustCompile(`(?i)(--env(?:=|\s+))([A-Za-z_][A-Za-z0-9_]*)(=)([^\s]+)`)
	envFileFlagPattern = regexp.MustCompile(`(?i)(--env-file(?:=|\s+))([^\s]+)`)
	secretFlagPattern  = regexp.MustCompile(`(?i)(--[A-Za-z0-9-]*(?:token|secret|password|api-key|apikey|authorization|credential|oauth)[A-Za-z0-9-]*)(=|\s+)([^\s]+)`)
	assignmentPattern  = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)=("[^"]*"|'[^']*'|[^\s]+)`)
)

// NewRedactionHandler wraps next and redacts secret-looking slog attributes
// before they are written to any underlying log sink.
func NewRedactionHandler(next slog.Handler) slog.Handler {
	return &redactionHandler{next: next}
}

type redactionHandler struct {
	next slog.Handler
}

func (h *redactionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactionHandler) Handle(ctx context.Context, rec slog.Record) error {
	redacted := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, redacted)
}

func (h *redactionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, redactAttr(attr))
	}
	return &redactionHandler{next: h.next.WithAttrs(redacted)}
}

func (h *redactionHandler) WithGroup(name string) slog.Handler {
	return &redactionHandler{next: h.next.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if isSensitiveKey(attr.Key) {
		attr.Value = slog.StringValue(redactedValue)
		return attr
	}

	switch attr.Value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(redactString(attr.Value.String()))
	case slog.KindGroup:
		group := attr.Value.Group()
		redacted := make([]slog.Attr, 0, len(group))
		for _, nested := range group {
			redacted = append(redacted, redactAttr(nested))
		}
		attr.Value = slog.GroupValue(redacted...)
	case slog.KindAny:
		attr.Value = slog.AnyValue(redactAny(attr.Value.Any(), attr.Key, 0))
	}
	return attr
}

func redactString(s string) string {
	s = envFlagPattern.ReplaceAllString(s, `${1}${2}${3}`+redactedValue)
	s = envFileFlagPattern.ReplaceAllString(s, `${1}`+redactedValue)
	s = secretFlagPattern.ReplaceAllString(s, `${1}${2}`+redactedValue)
	s = assignmentPattern.ReplaceAllStringFunc(s, func(match string) string {
		name, _, ok := strings.Cut(match, "=")
		if !ok || !isSensitiveName(name) {
			return match
		}
		return name + "=" + redactedValue
	})
	return s
}

func redactAny(v any, key string, depth int) any {
	if v == nil {
		return nil
	}
	if depth > 8 {
		return redactedValue
	}
	if isSensitiveKey(key) {
		return redactedValue
	}
	if err, ok := v.(error); ok {
		return redactString(err.Error())
	}

	rv := reflect.ValueOf(v)
	return redactReflectValue(rv, key, depth)
}

func redactReflectValue(rv reflect.Value, key string, depth int) any {
	if !rv.IsValid() {
		return nil
	}

	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.CanInterface() {
		if t, ok := rv.Interface().(fmt.Stringer); ok && isScalarStringer(rv) {
			return redactString(t.String())
		}
	}

	switch rv.Kind() {
	case reflect.String:
		return redactString(rv.String())
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Struct:
		return redactStruct(rv, depth)
	case reflect.Map:
		return redactMap(rv, depth)
	case reflect.Slice, reflect.Array:
		values := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			values = append(values, redactReflectValue(rv.Index(i), "", depth+1))
		}
		return values
	default:
		if rv.CanInterface() {
			return rv.Interface()
		}
		return fmt.Sprint(rv)
	}
}

func redactStruct(rv reflect.Value, depth int) any {
	if rv.Type().PkgPath() == "time" && rv.Type().Name() == "Time" && rv.CanInterface() {
		return rv.Interface()
	}

	fields := map[string]any{}
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue
		}

		name := field.Name
		if jsonName := strings.Split(field.Tag.Get("json"), ",")[0]; jsonName != "" && jsonName != "-" {
			name = jsonName
		}
		if isSensitiveKey(name) {
			fields[name] = redactedValue
			continue
		}
		fields[name] = redactReflectValue(rv.Field(i), name, depth+1)
	}
	return fields
}

func redactMap(rv reflect.Value, depth int) any {
	values := map[string]any{}
	iter := rv.MapRange()
	for iter.Next() {
		mapKey := fmt.Sprint(iter.Key().Interface())
		if isSensitiveKey(mapKey) {
			values[mapKey] = redactedValue
			continue
		}
		values[mapKey] = redactReflectValue(iter.Value(), mapKey, depth+1)
	}
	return values
}

func isScalarStringer(rv reflect.Value) bool {
	switch rv.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		return rv.Type().PkgPath() == "time" && rv.Type().Name() == "Time"
	default:
		return true
	}
}

func isSensitiveKey(key string) bool {
	return isSensitiveName(normalizeName(key))
}

func isSensitiveName(name string) bool {
	n := normalizeName(name)
	if n == "" {
		return false
	}
	if n == "envfile" || n == "authorization" {
		return true
	}
	return strings.Contains(n, "token") ||
		strings.Contains(n, "secret") ||
		strings.Contains(n, "password") ||
		strings.Contains(n, "apikey") ||
		strings.Contains(n, "accesskey") ||
		strings.Contains(n, "privatekey") ||
		strings.Contains(n, "credential") ||
		strings.Contains(n, "oauth")
}

func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, ".", "")
	return name
}
