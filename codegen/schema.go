package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func schemaJSON(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil {
		return ""
	}

	resolved := resolveSchema(ref, make(map[string]bool))
	if len(resolved) == 0 {
		return ""
	}

	data, err := json.MarshalIndent(resolved, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func resolveSchema(ref *openapi3.SchemaRef, seen map[string]bool) map[string]any {
	if ref == nil || ref.Value == nil {
		return nil
	}

	if ref.Ref != "" {
		if seen[ref.Ref] {
			return map[string]any{
				"description": fmt.Sprintf("Circular reference to %s", schemaNameFromRef(ref.Ref)),
			}
		}
		seen[ref.Ref] = true
		defer delete(seen, ref.Ref)
	}

	return resolveSchemaValue(ref.Value, seen)
}

func resolveSchemaValue(schema *openapi3.Schema, seen map[string]bool) map[string]any {
	if schema == nil {
		return nil
	}

	if _, baseRef := nullableVariant(schema.AnyOf); baseRef != nil {
		resolved := resolveSchema(baseRef, seen)
		if len(resolved) == 0 {
			resolved = map[string]any{}
		}
		resolved["nullable"] = true
		applySchemaMetadata(resolved, schema)
		return resolved
	}

	if len(schema.OneOf) > 0 {
		resolved := map[string]any{
			"oneOf": resolveSchemaList(schema.OneOf, seen),
		}
		applySchemaMetadata(resolved, schema)
		applyDiscriminator(resolved, schema.Discriminator)
		return resolved
	}

	if len(schema.AnyOf) > 0 {
		resolved := map[string]any{
			"anyOf": resolveSchemaList(schema.AnyOf, seen),
		}
		applySchemaMetadata(resolved, schema)
		return resolved
	}

	resolved := make(map[string]any)
	applySchemaMetadata(resolved, schema)

	switch {
	case schema.Type != nil && schema.Type.Is("object"), len(schema.Properties) > 0:
		resolved["type"] = "object"
		properties := make(map[string]any, len(schema.Properties))
		for _, name := range sortedMapKeys(schema.Properties) {
			properties[name] = resolveSchema(schema.Properties[name], seen)
		}
		resolved["properties"] = properties
		required := append([]string{}, schema.Required...)
		if required == nil {
			required = []string{}
		}
		resolved["required"] = required
	case schema.Type != nil && schema.Type.Is("array"):
		resolved["type"] = "array"
		if schema.Items != nil {
			resolved["items"] = resolveSchema(schema.Items, seen)
		}
	default:
		typeName, hasNull := schemaTypeName(schema)
		if typeName != "" {
			resolved["type"] = typeName
		}
		if hasNull {
			resolved["nullable"] = true
		}
	}

	return resolved
}

func resolveSchemaList(refs openapi3.SchemaRefs, seen map[string]bool) []any {
	resolved := make([]any, 0, len(refs))
	for _, ref := range refs {
		resolved = append(resolved, resolveSchema(ref, seen))
	}
	return resolved
}

func applySchemaMetadata(out map[string]any, schema *openapi3.Schema) {
	if schema == nil {
		return
	}

	if schema.Description != "" {
		out["description"] = schema.Description
	}
	if len(schema.Enum) > 0 {
		out["enum"] = append([]any{}, schema.Enum...)
	}
	if schema.Default != nil {
		out["default"] = schema.Default
	}
	if schema.Nullable {
		out["nullable"] = true
	}
}

func applyDiscriminator(out map[string]any, discriminator *openapi3.Discriminator) {
	if discriminator == nil {
		return
	}

	value := map[string]any{
		"propertyName": discriminator.PropertyName,
	}
	if len(discriminator.Mapping) > 0 {
		mapping := make(map[string]any, len(discriminator.Mapping))
		for key, target := range discriminator.Mapping {
			mapping[key] = target
		}
		value["mapping"] = mapping
	}
	out["discriminator"] = value
}

func nullableVariant(refs openapi3.SchemaRefs) (bool, *openapi3.SchemaRef) {
	if len(refs) != 2 {
		return false, nil
	}

	var base *openapi3.SchemaRef
	var sawNull bool
	for _, ref := range refs {
		if isNullSchema(ref) {
			sawNull = true
			continue
		}
		if base != nil {
			return false, nil
		}
		base = ref
	}
	return sawNull && base != nil, base
}

func isNullSchema(ref *openapi3.SchemaRef) bool {
	return ref != nil && ref.Value != nil && ref.Value.Type != nil && ref.Value.Type.Is("null")
}

func schemaTypeName(schema *openapi3.Schema) (typeName string, hasNull bool) {
	if schema == nil || schema.Type == nil {
		return "", false
	}
	for _, t := range schema.Type.Slice() {
		if t == "null" {
			hasNull = true
			continue
		}
		if typeName == "" {
			typeName = t
		}
	}
	return typeName, hasNull
}

func schemaNameFromRef(ref string) string {
	if idx := strings.LastIndex(ref, "/"); idx >= 0 && idx < len(ref)-1 {
		return ref[idx+1:]
	}
	return ref
}
