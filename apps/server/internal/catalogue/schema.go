/*
===========================================================================

schema.go - a JSON Schema for each catalogue file, from its Go type

The website generates its types from these schemas, so neither side keeps
a hand-copied shape: the schema is reflected from the same struct the
exporter encodes. A field tagged enum:"name" takes the named enum.

===========================================================================
*/
package catalogue

import (
	"reflect"
	"strings"
)

// schemaEnums are the enums a struct field can name with its enum tag.
var schemaEnums = map[string][]string{
	"status":     statusEnum,
	"race":       raceEnum,
	"gender":     genderEnum,
	"grade":      gradeEnum,
	"currency":   currencyEnum,
	"rarity":     rarityEnum,
	"searchKind": searchKindEnum,
	"noteArea":   noteAreaEnum,
}

/*
================
Schema

The draft 2020-12 schema of value's type, titled title.
================
*/
func Schema(title string, value any) map[string]any {
	schema := schemaOf(reflect.TypeOf(value), "")
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = title
	return schema
}

/*
================
schemaOf
================
*/
func schemaOf(t reflect.Type, enum string) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if values, ok := schemaEnums[enum]; ok {
		return map[string]any{"type": "string", "enum": values}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem(), "")}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem(), "")}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		addStructFields(t, properties, &required)
		return map[string]any{"type": "object", "properties": properties, "required": required,
			"additionalProperties": false}
	}
	return map[string]any{}
}

/*
================
addStructFields

Embedded structs contribute their fields inline, as encoding/json does.
A field without omitempty is required; a doc tag becomes its description.
================
*/
func addStructFields(t reflect.Type, properties map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			addStructFields(field.Type, properties, required)
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" || !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		property := schemaOf(field.Type, field.Tag.Get("enum"))
		if doc := field.Tag.Get("doc"); doc != "" {
			property["description"] = doc
		}
		properties[name] = property
		if !strings.Contains(options, "omitempty") {
			*required = append(*required, name)
		}
	}
}
