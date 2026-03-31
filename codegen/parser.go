package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// ParsedEndpoint is the intermediate representation of an API endpoint
// extracted from the OpenAPI spec.
type ParsedEndpoint struct {
	Path        string
	Method      string // "GET", "POST", etc.
	Tag         string
	Summary     string
	Description string
	OperationID string

	// Parameters
	PathParams  []ParsedParam
	QueryParams []ParsedParam

	// Request body
	ContentType  string // "application/json", "multipart/form-data", ""
	BodyFields   []ParsedField
	BodyRequired []string // required field names from the schema

	// Response analysis
	HasMore    bool   // response has has_more boolean field
	TokenField string // name of the pagination token field (next_token, token, cursor)
	DataField  string // name of the response array field (data, videos, etc.)
}

// ParsedParam represents a path or query parameter.
type ParsedParam struct {
	Name        string
	In          string // "path" or "query"
	Required    bool
	Type        string // Go type: "string", "int", "bool", "float64"
	Description string
	Default     interface{}
	Enum        []string
	Min         *int
	Max         *int
}

// ParsedField represents a field from a request body schema.
type ParsedField struct {
	Name        string
	Type        string // Go type: "string", "int", "bool", "float64", "string-slice"
	Description string
	Required    bool
	Default     interface{}
	Enum        []string
	Min         *int
	Max         *int
	Complex     bool // true for objects, discriminated unions, arrays of objects
}

// Parse reads an OpenAPI JSON spec using kin-openapi and returns parsed endpoints.
// Filters to v3 endpoints by default. Respects x-cli-visible: false to exclude
// individual endpoints.
func Parse(specPath string) ([]ParsedEndpoint, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("loading spec: %w", err)
	}

	// No strict validation — kin-openapi resolves $ref and parses schemas
	// correctly without it, and real-world specs have quirks the validator rejects.

	var endpoints []ParsedEndpoint

	// Sort paths for deterministic output
	paths := make([]string, 0, len(doc.Paths.Map()))
	for path := range doc.Paths.Map() {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pathItem := doc.Paths.Map()[path]
		operations := pathItem.Operations()

		// Sort methods for deterministic output
		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, method)
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := operations[method]

			// Skip endpoints marked as not CLI-visible.
			// When x-cli-visible is absent, default to including v3 endpoints only.
			if visible, ok := op.Extensions["x-cli-visible"]; ok {
				if v, ok := visible.(bool); ok && !v {
					continue
				}
			} else if !strings.HasPrefix(path, "/v3/") {
				continue
			}

			ep := ParsedEndpoint{
				Path:        path,
				Method:      strings.ToUpper(method),
				Summary:     op.Summary,
				Description: op.Description,
				OperationID: op.OperationID,
			}

			// Tag
			if len(op.Tags) > 0 {
				ep.Tag = op.Tags[0]
			}

			// Parameters (includes path-level params merged by kin-openapi)
			for _, pRef := range op.Parameters {
				p := pRef.Value
				if p == nil {
					continue
				}
				parsed := parseParam(p)
				switch p.In {
				case "path":
					ep.PathParams = append(ep.PathParams, parsed)
				case "query":
					ep.QueryParams = append(ep.QueryParams, parsed)
				}
			}

			// Request body
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				parseBody(op.RequestBody.Value, &ep)
			}

			// Response analysis (pagination + data field)
			if resp := op.Responses.Status(200); resp != nil && resp.Value != nil {
				analyzeResponse(resp.Value, &ep)
			}

			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

func parseParam(p *openapi3.Parameter) ParsedParam {
	param := ParsedParam{
		Name:        p.Name,
		In:          p.In,
		Required:    p.Required,
		Description: p.Description,
	}

	if p.Schema != nil && p.Schema.Value != nil {
		s := p.Schema.Value
		param.Type = mapSchemaType(s)
		param.Default = s.Default
		param.Enum = extractEnum(s)
		if s.Min != nil {
			v := int(*s.Min)
			param.Min = &v
		}
		if s.Max != nil {
			v := int(*s.Max)
			param.Max = &v
		}
	}

	return param
}

func parseBody(rb *openapi3.RequestBody, ep *ParsedEndpoint) {
	// Check content types
	for ct, media := range rb.Content {
		ep.ContentType = ct

		if ct == "multipart/form-data" {
			return
		}

		if ct != "application/json" || media.Schema == nil || media.Schema.Value == nil {
			continue
		}

		schema := media.Schema.Value

		// Extract fields from properties (sorted for deterministic output)
		propNames := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			propNames = append(propNames, name)
		}
		sort.Strings(propNames)

		for _, name := range propNames {
			propRef := schema.Properties[name]
			if propRef == nil || propRef.Value == nil {
				continue
			}
			field := classifyField(name, propRef.Value, schema.Required)
			ep.BodyFields = append(ep.BodyFields, field)
		}
		ep.BodyRequired = schema.Required

		return
	}
}

func classifyField(name string, schema *openapi3.Schema, required []string) ParsedField {
	field := ParsedField{
		Name:        name,
		Description: schema.Description,
		Required:    containsStr(required, name),
		Default:     schema.Default,
	}

	// Handle Pydantic nullable pattern: anyOf: [{type: T}, {type: null}]
	if len(schema.AnyOf) > 0 {
		nonNull := extractNonNull(schema.AnyOf)
		if nonNull != nil {
			if isComplexSchema(nonNull) {
				field.Complex = true
				field.Type = "string"
				return field
			}
			field.Type = mapSchemaType(nonNull)
			field.Enum = extractEnum(nonNull)
			if nonNull.Min != nil {
				v := int(*nonNull.Min)
				field.Min = &v
			}
			if nonNull.Max != nil {
				v := int(*nonNull.Max)
				field.Max = &v
			}
			return field
		}
	}

	// Handle oneOf (discriminated unions) — these are complex
	if len(schema.OneOf) > 0 || schema.Discriminator != nil {
		field.Complex = true
		field.Type = "string"
		return field
	}

	if isComplexSchema(schema) {
		field.Complex = true
		field.Type = "string"
		return field
	}

	field.Type = mapSchemaType(schema)
	field.Enum = extractEnum(schema)
	if schema.Min != nil {
		v := int(*schema.Min)
		field.Min = &v
	}
	if schema.Max != nil {
		v := int(*schema.Max)
		field.Max = &v
	}

	return field
}

// extractNonNull extracts the non-null schema from an anyOf array.
func extractNonNull(anyOf openapi3.SchemaRefs) *openapi3.Schema {
	var nonNulls []*openapi3.Schema
	for _, ref := range anyOf {
		if ref.Value == nil {
			continue
		}
		s := ref.Value
		if s.Type != nil && s.Type.Is("null") {
			continue
		}
		nonNulls = append(nonNulls, s)
	}
	if len(nonNulls) == 1 {
		return nonNulls[0]
	}
	return nil
}

func isComplexSchema(schema *openapi3.Schema) bool {
	if schema == nil {
		return false
	}

	// Object types are complex
	if schema.Type != nil && schema.Type.Is("object") {
		return true
	}

	// Discriminated unions are complex
	if schema.Discriminator != nil || len(schema.OneOf) > 0 {
		return true
	}

	// Array of objects are complex
	if schema.Type != nil && schema.Type.Is("array") && schema.Items != nil && schema.Items.Value != nil {
		items := schema.Items.Value
		if items.Type != nil && items.Type.Is("object") {
			return true
		}
		if items.Discriminator != nil || len(items.OneOf) > 0 {
			return true
		}
	}

	return false
}

func mapSchemaType(schema *openapi3.Schema) string {
	if schema == nil || schema.Type == nil {
		return "string"
	}

	t := schema.Type.Slice()
	if len(t) == 0 {
		return "string"
	}

	// Handle nullable types: ["string", "null"] → "string"
	primary := t[0]
	for _, typ := range t {
		if typ != "null" {
			primary = typ
			break
		}
	}

	switch primary {
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		if schema.Items != nil && schema.Items.Value != nil {
			items := schema.Items.Value
			if items.Type != nil && items.Type.Is("string") {
				return "string-slice"
			}
		}
		return "string-slice"
	default:
		return "string"
	}
}

func extractEnum(schema *openapi3.Schema) []string {
	if len(schema.Enum) == 0 {
		return nil
	}
	var result []string
	for _, v := range schema.Enum {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func analyzeResponse(resp *openapi3.Response, ep *ParsedEndpoint) {
	media := resp.Content.Get("application/json")
	if media == nil || media.Schema == nil || media.Schema.Value == nil {
		return
	}

	schema := media.Schema.Value

	// Check for has_more (pagination indicator)
	if prop, ok := schema.Properties["has_more"]; ok && prop.Value != nil {
		if prop.Value.Type != nil && prop.Value.Type.Is("boolean") {
			ep.HasMore = true
		}
	}

	// Check for token field
	for _, name := range []string{"next_token", "token", "cursor"} {
		if _, ok := schema.Properties[name]; ok {
			ep.TokenField = name
			break
		}
	}

	// Check for data array field
	for name, prop := range schema.Properties {
		if prop.Value != nil && prop.Value.Type != nil && prop.Value.Type.Is("array") {
			ep.DataField = name
			break
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
