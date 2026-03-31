package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
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

// openAPISpec is a minimal representation of the OpenAPI document.
type openAPISpec struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

// operationDetail captures what we need from an operation.
type operationDetail struct {
	Tags        []string                       `json:"tags"`
	Summary     string                         `json:"summary"`
	Description string                         `json:"description"`
	OperationID string                         `json:"operationId"`
	Parameters  []parameterDetail              `json:"parameters"`
	RequestBody *requestBodyDetail             `json:"requestBody"`
	Responses   map[string]responseDetail      `json:"responses"`
}

type parameterDetail struct {
	Name        string          `json:"name"`
	In          string          `json:"in"`
	Required    bool            `json:"required"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type requestBodyDetail struct {
	Content map[string]mediaTypeDetail `json:"content"`
}

type mediaTypeDetail struct {
	Schema json.RawMessage `json:"schema"`
}

type responseDetail struct {
	Content map[string]mediaTypeDetail `json:"content"`
}

// schemaObj is a flexible representation of an OpenAPI schema.
type schemaObj struct {
	Ref                  string                `json:"$ref"`
	Type                 interface{}           `json:"type"` // string or []string
	Format               string                `json:"format"`
	Description          string                `json:"description"`
	Default              interface{}           `json:"default"`
	Enum                 []interface{}          `json:"enum"`
	Minimum              *float64              `json:"minimum"`
	Maximum              *float64              `json:"maximum"`
	MinLength            *int                  `json:"minLength"`
	MaxLength            *int                  `json:"maxLength"`
	Properties           map[string]schemaObj  `json:"properties"`
	Required             []string              `json:"required"`
	Items                *schemaObj            `json:"items"`
	AnyOf                []schemaObj           `json:"anyOf"`
	OneOf                []schemaObj           `json:"oneOf"`
	Discriminator        *discriminatorObj     `json:"discriminator"`
	AdditionalProperties interface{}           `json:"additionalProperties"`
}

type discriminatorObj struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping"`
}

// Parse reads an OpenAPI JSON spec and returns parsed endpoints.
func Parse(specPath string) ([]ParsedEndpoint, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	var spec openAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing spec JSON: %w", err)
	}

	var endpoints []ParsedEndpoint

	// Sort paths for deterministic output
	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		methods := spec.Paths[path]
		// Sort methods for deterministic output
		methodNames := make([]string, 0, len(methods))
		for method := range methods {
			methodNames = append(methodNames, method)
		}
		sort.Strings(methodNames)

		for _, method := range methodNames {
			raw := methods[method]
			// Skip non-operation keys like "parameters"
			if !isHTTPMethod(method) {
				continue
			}

			var op operationDetail
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("parsing operation %s %s: %w", method, path, err)
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

			// Parameters
			for _, p := range op.Parameters {
				parsed := parseParameter(p)
				switch p.In {
				case "path":
					ep.PathParams = append(ep.PathParams, parsed)
				case "query":
					ep.QueryParams = append(ep.QueryParams, parsed)
				}
			}

			// Request body
			if op.RequestBody != nil {
				parseRequestBody(op.RequestBody, &ep, spec.Components.Schemas)
			}

			// Response analysis (pagination + data field)
			if resp, ok := op.Responses["200"]; ok {
				analyzeResponse(&resp, &ep)
			}

			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

func isHTTPMethod(m string) bool {
	switch strings.ToLower(m) {
	case "get", "post", "put", "patch", "delete":
		return true
	}
	return false
}

func parseParameter(p parameterDetail) ParsedParam {
	param := ParsedParam{
		Name:        p.Name,
		In:          p.In,
		Required:    p.Required,
		Description: p.Description,
	}

	var schema schemaObj
	if err := json.Unmarshal(p.Schema, &schema); err == nil {
		param.Type = mapOpenAPIType(&schema)
		param.Default = schema.Default
		param.Enum = extractEnumStrings(schema.Enum)
		if schema.Minimum != nil {
			v := int(*schema.Minimum)
			param.Min = &v
		}
		if schema.Maximum != nil {
			v := int(*schema.Maximum)
			param.Max = &v
		}
	}

	return param
}

func parseRequestBody(rb *requestBodyDetail, ep *ParsedEndpoint, schemas map[string]json.RawMessage) {
	// Check content types in preference order
	for ct, media := range rb.Content {
		ep.ContentType = ct

		if ct == "multipart/form-data" {
			// For multipart, we don't extract fields — the file arg is handled by overrides
			return
		}

		// Parse the schema (resolve $ref if needed)
		var schema schemaObj
		if err := json.Unmarshal(media.Schema, &schema); err != nil {
			return
		}

		// Resolve $ref
		resolved := resolveSchema(schema, schemas)

		// Extract fields from properties (sorted for deterministic output)
		propNames := make([]string, 0, len(resolved.Properties))
		for name := range resolved.Properties {
			propNames = append(propNames, name)
		}
		sort.Strings(propNames)
		for _, name := range propNames {
			propSchema := resolved.Properties[name]
			field := classifyField(name, propSchema, resolved.Required, schemas)
			ep.BodyFields = append(ep.BodyFields, field)
		}
		ep.BodyRequired = resolved.Required

		return // Only process the first content type
	}
}

func resolveSchema(schema schemaObj, schemas map[string]json.RawMessage) schemaObj {
	if schema.Ref != "" {
		name := refName(schema.Ref)
		if raw, ok := schemas[name]; ok {
			var resolved schemaObj
			if err := json.Unmarshal(raw, &resolved); err == nil {
				return resolved
			}
		}
	}
	return schema
}

func refName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

func classifyField(name string, schema schemaObj, required []string, schemas map[string]json.RawMessage) ParsedField {
	field := ParsedField{
		Name:        name,
		Description: schema.Description,
		Required:    containsStr(required, name),
		Default:     schema.Default,
	}

	// Handle Pydantic nullable pattern: anyOf: [{type: T}, {type: null}]
	if len(schema.AnyOf) > 0 {
		nonNull := extractNonNull(schema.AnyOf, schemas)
		if nonNull != nil {
			// Check if the non-null type is complex
			if isComplexSchema(nonNull, schemas) {
				field.Complex = true
				field.Type = "string" // placeholder
				return field
			}
			field.Type = mapOpenAPIType(nonNull)
			field.Enum = extractEnumStrings(nonNull.Enum)
			if nonNull.Minimum != nil {
				v := int(*nonNull.Minimum)
				field.Min = &v
			}
			if nonNull.Maximum != nil {
				v := int(*nonNull.Maximum)
				field.Max = &v
			}
			return field
		}
	}

	// Handle oneOf (discriminated unions) — these are complex
	if len(schema.OneOf) > 0 || schema.Discriminator != nil {
		field.Complex = true
		field.Type = "string" // placeholder
		return field
	}

	// Check if it's a complex type directly
	if isComplexSchema(&schema, schemas) {
		field.Complex = true
		field.Type = "string" // placeholder
		return field
	}

	field.Type = mapOpenAPIType(&schema)
	field.Enum = extractEnumStrings(schema.Enum)
	if schema.Minimum != nil {
		v := int(*schema.Minimum)
		field.Min = &v
	}
	if schema.Maximum != nil {
		v := int(*schema.Maximum)
		field.Max = &v
	}

	return field
}

// extractNonNull extracts the non-null schema from an anyOf array.
// Returns nil if no non-null type is found or if there are multiple non-null types.
func extractNonNull(anyOf []schemaObj, schemas map[string]json.RawMessage) *schemaObj {
	var nonNulls []schemaObj
	for _, s := range anyOf {
		if getTypeString(s.Type) == "null" {
			continue
		}
		nonNulls = append(nonNulls, s)
	}
	if len(nonNulls) == 1 {
		result := nonNulls[0]
		// Resolve $ref if present
		if result.Ref != "" {
			resolved := resolveSchema(result, schemas)
			return &resolved
		}
		return &result
	}
	return nil
}

// isComplexSchema returns true for schemas that can't be represented as simple flags.
func isComplexSchema(schema *schemaObj, schemas map[string]json.RawMessage) bool {
	if schema == nil {
		return false
	}

	// Resolve $ref
	if schema.Ref != "" {
		resolved := resolveSchema(*schema, schemas)
		schema = &resolved
	}

	t := getTypeString(schema.Type)

	// Object types are complex
	if t == "object" {
		return true
	}

	// Discriminated unions are complex
	if schema.Discriminator != nil || len(schema.OneOf) > 0 {
		return true
	}

	// Array of objects are complex
	if t == "array" && schema.Items != nil {
		if schema.Items.Ref != "" {
			resolved := resolveSchema(*schema.Items, schemas)
			if getTypeString(resolved.Type) == "object" || resolved.Discriminator != nil || len(resolved.OneOf) > 0 {
				return true
			}
		}
		if getTypeString(schema.Items.Type) == "object" || schema.Items.Discriminator != nil || len(schema.Items.OneOf) > 0 {
			return true
		}
	}

	return false
}

func mapOpenAPIType(schema *schemaObj) string {
	if schema == nil {
		return "string"
	}

	t := getTypeString(schema.Type)

	switch t {
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		// Check items type
		if schema.Items != nil {
			itemType := getTypeString(schema.Items.Type)
			if itemType == "string" {
				return "string-slice"
			}
		}
		return "string-slice" // default for arrays
	default:
		return "string"
	}
}

func getTypeString(t interface{}) string {
	switch v := t.(type) {
	case string:
		return v
	case []interface{}:
		// Handle type: ["string", "null"] — return the non-null type
		for _, item := range v {
			if s, ok := item.(string); ok && s != "null" {
				return s
			}
		}
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return "string"
}

func extractEnumStrings(enum []interface{}) []string {
	if len(enum) == 0 {
		return nil
	}
	var result []string
	for _, v := range enum {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func analyzeResponse(resp *responseDetail, ep *ParsedEndpoint) {
	media, ok := resp.Content["application/json"]
	if !ok {
		return
	}

	var schema schemaObj
	if err := json.Unmarshal(media.Schema, &schema); err != nil {
		return
	}

	// Check for has_more (pagination indicator)
	if prop, ok := schema.Properties["has_more"]; ok {
		if getTypeString(prop.Type) == "boolean" {
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
		if getTypeString(prop.Type) == "array" {
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
