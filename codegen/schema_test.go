package main

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestResolveSchema_SimpleObject(t *testing.T) {
	ref := openapi3.NewSchemaRef("", openapi3.NewObjectSchema().
		WithProperty("name", openapi3.NewStringSchema()).
		WithProperty("count", openapi3.NewIntegerSchema()).
		WithRequired([]string{"name"}))
	ref.Value.Description = "top-level"
	ref.Value.Properties["name"].Value.Description = "display name"

	got := resolveSchema(ref, map[string]bool{})

	if got["type"] != "object" {
		t.Fatalf("type = %v, want object", got["type"])
	}
	if got["description"] != "top-level" {
		t.Fatalf("description = %v, want top-level", got["description"])
	}
	required, ok := got["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "name" {
		t.Fatalf("required = %#v, want [name]", got["required"])
	}
	properties := got["properties"].(map[string]any)
	name := properties["name"].(map[string]any)
	if name["type"] != "string" || name["description"] != "display name" {
		t.Fatalf("name schema = %#v", name)
	}
}

func TestResolveSchema_NestedObject(t *testing.T) {
	child := openapi3.NewObjectSchema().
		WithProperty("enabled", openapi3.NewBoolSchema()).
		WithRequired([]string{"enabled"})
	root := openapi3.NewObjectSchema().
		WithProperty("settings", child)

	got := resolveSchema(openapi3.NewSchemaRef("", root), map[string]bool{})
	properties := got["properties"].(map[string]any)
	settings := properties["settings"].(map[string]any)

	if settings["type"] != "object" {
		t.Fatalf("settings.type = %v, want object", settings["type"])
	}
	required := settings["required"].([]string)
	if len(required) != 1 || required[0] != "enabled" {
		t.Fatalf("settings.required = %#v, want [enabled]", required)
	}
}

func TestResolveSchema_Array(t *testing.T) {
	ref := openapi3.NewSchemaRef("", openapi3.NewArraySchema().WithItems(openapi3.NewStringSchema()))

	got := resolveSchema(ref, map[string]bool{})

	if got["type"] != "array" {
		t.Fatalf("type = %v, want array", got["type"])
	}
	items := got["items"].(map[string]any)
	if items["type"] != "string" {
		t.Fatalf("items.type = %v, want string", items["type"])
	}
}

func TestResolveSchema_NullableFields(t *testing.T) {
	nullable := &openapi3.Schema{
		AnyOf: openapi3.SchemaRefs{
			openapi3.NewSchemaRef("", openapi3.NewStringSchema()),
			openapi3.NewSchemaRef("", &openapi3.Schema{Type: &openapi3.Types{"null"}}),
		},
		Description: "optional name",
	}

	got := resolveSchema(openapi3.NewSchemaRef("", nullable), map[string]bool{})

	if got["type"] != "string" {
		t.Fatalf("type = %v, want string", got["type"])
	}
	if got["nullable"] != true {
		t.Fatalf("nullable = %v, want true", got["nullable"])
	}
	if got["description"] != "optional name" {
		t.Fatalf("description = %v, want optional name", got["description"])
	}
}

func TestResolveSchema_NullableTypeArray(t *testing.T) {
	schema := &openapi3.Schema{
		Type:        &openapi3.Types{"string", "null"},
		Description: "cursor token",
	}

	got := resolveSchema(openapi3.NewSchemaRef("", schema), map[string]bool{})

	if got["type"] != "string" {
		t.Fatalf("type = %v, want string", got["type"])
	}
	if got["nullable"] != true {
		t.Fatalf("nullable = %v, want true", got["nullable"])
	}
	if got["description"] != "cursor token" {
		t.Fatalf("description = %v, want cursor token", got["description"])
	}
}

func TestResolveSchema_ObjectWithNullableTypeArrayField(t *testing.T) {
	root := openapi3.NewObjectSchema().
		WithProperty("has_more", openapi3.NewBoolSchema()).
		WithRequired([]string{"has_more"})
	root.Properties["next_token"] = openapi3.NewSchemaRef("", &openapi3.Schema{
		Type:        &openapi3.Types{"string", "null"},
		Description: "Opaque cursor for the next page",
	})

	got := resolveSchema(openapi3.NewSchemaRef("", root), map[string]bool{})
	properties := got["properties"].(map[string]any)
	nextToken := properties["next_token"].(map[string]any)

	if nextToken["type"] != "string" {
		t.Fatalf("next_token.type = %v, want string", nextToken["type"])
	}
	if nextToken["nullable"] != true {
		t.Fatalf("next_token.nullable = %v, want true", nextToken["nullable"])
	}

	j := schemaJSON(openapi3.NewSchemaRef("", root))
	if strings.Contains(j, "string|null") {
		t.Fatalf("schemaJSON contains invalid 'string|null':\n%s", j)
	}
}

func TestResolveSchema_EnumValues(t *testing.T) {
	ref := openapi3.NewSchemaRef("", openapi3.NewStringSchema().WithEnum("draft", "ready"))

	got := resolveSchema(ref, map[string]bool{})

	enum, ok := got["enum"].([]any)
	if !ok || len(enum) != 2 || enum[0] != "draft" || enum[1] != "ready" {
		t.Fatalf("enum = %#v, want [draft ready]", got["enum"])
	}
}

func TestResolveSchema_Ref(t *testing.T) {
	user := openapi3.NewObjectSchema().
		WithProperty("id", openapi3.NewStringSchema()).
		WithRequired([]string{"id"})
	ref := &openapi3.SchemaRef{
		Ref:   "#/components/schemas/User",
		Value: user,
	}

	got := resolveSchema(ref, map[string]bool{})
	properties := got["properties"].(map[string]any)
	id := properties["id"].(map[string]any)
	if id["type"] != "string" {
		t.Fatalf("id.type = %v, want string", id["type"])
	}
}

func TestResolveSchema_OneOfDiscriminator(t *testing.T) {
	first := openapi3.NewObjectSchema().WithProperty("kind", openapi3.NewStringSchema()).WithProperty("url", openapi3.NewStringSchema())
	second := openapi3.NewObjectSchema().WithProperty("kind", openapi3.NewStringSchema()).WithProperty("asset_id", openapi3.NewStringSchema())
	ref := openapi3.NewSchemaRef("", &openapi3.Schema{
		OneOf: openapi3.SchemaRefs{
			openapi3.NewSchemaRef("#/components/schemas/UrlSource", first),
			openapi3.NewSchemaRef("#/components/schemas/AssetSource", second),
		},
		Discriminator: &openapi3.Discriminator{
			PropertyName: "kind",
		},
	})

	got := resolveSchema(ref, map[string]bool{})

	oneOf, ok := got["oneOf"].([]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("oneOf = %#v, want 2 variants", got["oneOf"])
	}
	discriminator := got["discriminator"].(map[string]any)
	if discriminator["propertyName"] != "kind" {
		t.Fatalf("discriminator = %#v, want propertyName=kind", discriminator)
	}
}
