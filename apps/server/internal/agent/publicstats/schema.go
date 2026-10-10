/*
===========================================================================

schema.go - GET /public/v1/schema: JSON Schema from the response structs

The site generates its validators from this document, so no shape is ever
copied by hand between Go and TypeScript. It is built by reflection from
the exact types the handlers encode: a field added, renamed or retyped in
responses.go changes the published schema with it.

===========================================================================
*/
package publicstats

import (
	"net/http"
	"reflect"
	"strings"
)

// endpointTypes maps each endpoint to the type its answer encodes.
var endpointTypes = []struct {
	path string
	typ  reflect.Type
}{
	{"/public/v1/uniques", reflect.TypeOf(UniquesResponse{})},
	{"/public/v1/uniques/kills", reflect.TypeOf(KillsResponse{})},
	{"/public/v1/leaderboards/uniques", reflect.TypeOf(LeaderboardResponse{})},
	{"/public/v1/firsts", reflect.TypeOf(FirstsResponse{})},
	{"/public/v1/characters", reflect.TypeOf(CharactersResponse{})},
	{"/public/v1/characters/{name}", reflect.TypeOf(Profile{})},
	{"/public/v1/rules", reflect.TypeOf(RulesResponse{})},
}

/*
================
schema

The schema document: one JSON Schema (draft 2020-12) per endpoint.
================
*/
func (s *Service) schema(*http.Request) (any, error) {
	return SchemaDocument(), nil
}

/*
================
SchemaDocument
================
*/
func SchemaDocument() map[string]any {
	endpoints := map[string]any{}
	for _, endpoint := range endpointTypes {
		root := typeSchema(endpoint.typ)
		root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		endpoints[endpoint.path] = root
	}
	return map[string]any{"version": SchemaVersion, "endpoints": endpoints}
}

/*
================
typeSchema

The schema of one Go type as encoding/json writes it. A pointer is
nullable; omitempty fields are optional, every other field is required.
================
*/
func typeSchema(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		inner := typeSchema(t.Elem())
		return map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out := map[string]any{"type": "integer"}
		if t.Kind() >= reflect.Uint && t.Kind() <= reflect.Uint64 {
			out["minimum"] = 0
		}
		return out
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": typeSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": typeSchema(t.Elem())}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = typeSchema(field.Type)
			if !strings.Contains(options, "omitempty") {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	return map[string]any{}
}
