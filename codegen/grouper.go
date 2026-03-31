package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// CommandGroup represents a group of CLI commands (e.g., "video", "avatar").
type CommandGroup struct {
	Name     string           // group name, lowercase ("video", "avatar")
	Commands []GroupedCommand // subcommands
}

// GroupedCommand represents a single CLI subcommand within a group.
type GroupedCommand struct {
	// CLI presentation
	Group       string
	Name        string // subcommand name ("list", "get", "look-get")
	Summary     string
	Description string
	Examples    []string

	// Arguments and flags
	Args  []ArgDef
	Flags []FlagDef

	// HTTP identity
	Endpoint     string
	Method       string
	BodyEncoding string // "json", "multipart", ""

	// Execution behavior
	TokenField string
	DataField  string

	// Variable name for codegen
	VarName string // PascalCase: "VideoList", "AvatarLookGet"
}

// ArgDef mirrors command.ArgSpec for codegen output.
type ArgDef struct {
	Name   string // display name, kebab-case ("video-id")
	Target string // "path", "body", "file"
	Param  string // target key
	Help   string
}

// FlagDef mirrors command.FlagSpec for codegen output.
type FlagDef struct {
	Name     string
	Type     string
	Default  string
	Help     string
	Required bool
	Enum     []string
	Min      *int
	Max      *int
	Source   string // "query" or "body"
	JSONName string
}

// GroupEndpoints walks the OpenAPI document, filters endpoints, and groups them
// into CLI command groups. It replaces the previous two-step Parse+Group flow
// by working directly with kin-openapi types.
func GroupEndpoints(doc *openapi3.T, overrides *Overrides) []CommandGroup {
	type parsedOp struct {
		path        string
		method      string
		op          *openapi3.Operation
		pathItem    *openapi3.PathItem
		tag         string
		contentType string
	}

	// Collect visible v3 operations, sorted by path then method for determinism.
	paths := sortedPaths(doc)
	var ops []parsedOp
	for _, path := range paths {
		pathItem := doc.Paths.Find(path)
		if pathItem == nil {
			continue
		}
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			op := pathItem.GetOperation(method)
			if op == nil {
				continue
			}

			// Filter: v3 only
			if !strings.HasPrefix(path, "/v3/") {
				continue
			}

			// Filter: x-cli-visible
			if vis, ok := op.Extensions["x-cli-visible"]; ok {
				if b, ok := vis.(bool); ok && !b {
					continue
				}
			}

			tag := "Other"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}

			ct := detectContentType(op)

			ops = append(ops, parsedOp{
				path:        path,
				method:      method,
				op:          op,
				pathItem:    pathItem,
				tag:         tag,
				contentType: ct,
			})
		}
	}

	// Group by tag
	type tagOps struct {
		tag string
		ops []parsedOp
	}
	byTag := make(map[string]*tagOps)
	var tagOrder []string
	for _, o := range ops {
		if _, ok := byTag[o.tag]; !ok {
			byTag[o.tag] = &tagOps{tag: o.tag}
			tagOrder = append(tagOrder, o.tag)
		}
		byTag[o.tag].ops = append(byTag[o.tag].ops, o)
	}

	// Build command groups
	var groups []CommandGroup
	for _, tag := range tagOrder {
		to := byTag[tag]
		groupName := deriveGroupName(to.tag, overrides)
		group := CommandGroup{Name: groupName}

		for _, o := range to.ops {
			cmd := buildCommandFromOp(o.path, o.method, o.op, o.pathItem, o.contentType, groupName, doc, overrides)
			group.Commands = append(group.Commands, cmd)
		}

		// Sort commands for deterministic output
		sort.Slice(group.Commands, func(i, j int) bool {
			return group.Commands[i].Name < group.Commands[j].Name
		})

		groups = append(groups, group)
	}

	// Sort groups for deterministic output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	return groups
}

// sortedPaths returns all paths in deterministic order.
func sortedPaths(doc *openapi3.T) []string {
	if doc.Paths == nil {
		return nil
	}
	var paths []string
	for path := range doc.Paths.Map() {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// detectContentType returns the first content type from the request body, or "".
func detectContentType(op *openapi3.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return ""
	}
	// Prefer multipart, then JSON
	if _, ok := op.RequestBody.Value.Content["multipart/form-data"]; ok {
		return "multipart/form-data"
	}
	if _, ok := op.RequestBody.Value.Content["application/json"]; ok {
		return "application/json"
	}
	for ct := range op.RequestBody.Value.Content {
		return ct
	}
	return ""
}

// buildCommandFromOp constructs a GroupedCommand directly from kin-openapi types.
func buildCommandFromOp(
	path, method string,
	op *openapi3.Operation,
	pathItem *openapi3.PathItem,
	contentType, groupName string,
	doc *openapi3.T,
	overrides *Overrides,
) GroupedCommand {
	// Derive subcommand name using path/method/contentType
	subName := deriveSubcommandNameFromPath(path, method, contentType, groupName)
	key := method + " " + path

	cmd := GroupedCommand{
		Group:       groupName,
		Name:        subName,
		Summary:     op.Summary,
		Description: op.Description,
		Endpoint:    path,
		Method:      method,
		VarName:     toPascalCase(groupName) + toPascalCase(subName),
	}

	// Examples from overrides
	if examples, ok := overrides.Examples[key]; ok {
		cmd.Examples = examples
	}

	// Body encoding
	switch contentType {
	case "application/json":
		cmd.BodyEncoding = "json"
	case "multipart/form-data":
		cmd.BodyEncoding = "multipart"
	default:
		cmd.BodyEncoding = ""
	}

	// Pagination detection from response schema
	hasMore, tokenField, dataField := detectPagination(op, doc)
	if hasMore && tokenField != "" {
		cmd.TokenField = tokenField
	}
	cmd.DataField = dataField

	// Build positional override lookup
	positionalOverrides := overrides.GetPositionalOverrides(key)
	positionalByField := make(map[string]PositionalOverride)
	for _, po := range positionalOverrides {
		positionalByField[po.Field] = po
	}

	// Path params → ArgDefs
	for _, paramRef := range collectParams(pathItem, op) {
		param := paramRef.Value
		if param == nil || param.In != "path" {
			continue
		}
		argName := toKebabCase(param.Name)
		cmd.Args = append(cmd.Args, ArgDef{
			Name:   argName,
			Target: "path",
			Param:  param.Name,
			Help:   param.Description,
		})
	}

	// Check for positional overrides that promote body/file fields to args
	for _, po := range positionalOverrides {
		switch po.Target {
		case "body":
			help := ""
			bodySchema := requestBodySchema(op, contentType)
			if bodySchema != nil {
				if prop := bodySchema.Properties[po.Field]; prop != nil && prop.Value != nil {
					help = prop.Value.Description
				}
			}
			cmd.Args = append(cmd.Args, ArgDef{
				Name:   toKebabCase(po.Field),
				Target: "body",
				Param:  po.Field,
				Help:   help,
			})
		case "file":
			cmd.Args = append(cmd.Args, ArgDef{
				Name:   toKebabCase(po.Field),
				Target: "file",
				Param:  po.Field,
				Help:   "File path for upload",
			})
		}
	}

	// Query params → FlagDefs
	for _, paramRef := range collectParams(pathItem, op) {
		param := paramRef.Value
		if param == nil || param.In != "query" {
			continue
		}
		flag := FlagDef{
			Name:     safeFlagName(toKebabCase(param.Name)),
			Type:     schemaToType(param.Schema),
			Help:     param.Description,
			Required: param.Required,
			Source:   "query",
			JSONName: param.Name,
		}
		if param.Schema != nil && param.Schema.Value != nil {
			s := param.Schema.Value
			flag.Enum = schemaEnum(s)
			flag.Min = floatPtrToIntPtr(s.Min)
			flag.Max = floatPtrToIntPtr(s.Max)
			if s.Default != nil {
				flag.Default = formatDefault(s.Default)
			}
		}
		cmd.Flags = append(cmd.Flags, flag)
	}

	// Body fields → FlagDefs (skip complex fields and positional overrides)
	bodySchema := requestBodySchema(op, contentType)
	if bodySchema != nil {
		required := make(map[string]bool)
		for _, r := range bodySchema.Required {
			required[r] = true
		}

		// Sort property names for deterministic output
		propNames := sortedPropertyNames(bodySchema)

		for _, fieldName := range propNames {
			propRef := bodySchema.Properties[fieldName]
			if propRef == nil || propRef.Value == nil {
				continue
			}
			prop := propRef.Value

			// Skip complex fields — they're covered by --json-body
			if isComplexField(prop) {
				continue
			}
			// Skip fields promoted to positional args
			if _, ok := positionalByField[fieldName]; ok {
				continue
			}

			flag := FlagDef{
				Name:     safeFlagName(toKebabCase(fieldName)),
				Type:     schemaToType(propRef),
				Help:     prop.Description,
				Required: required[fieldName],
				Enum:     schemaEnum(prop),
				Min:      floatPtrToIntPtr(prop.Min),
				Max:      floatPtrToIntPtr(prop.Max),
				Source:   "body",
				JSONName: fieldName,
			}
			if prop.Default != nil {
				flag.Default = formatDefault(prop.Default)
			}
			cmd.Flags = append(cmd.Flags, flag)
		}
	}

	return cmd
}

// collectParams merges path-item level and operation-level params, with
// operation-level taking precedence.
func collectParams(pathItem *openapi3.PathItem, op *openapi3.Operation) openapi3.Parameters {
	seen := make(map[string]bool)
	var result openapi3.Parameters

	// Operation params first (higher precedence)
	for _, p := range op.Parameters {
		if p.Value != nil {
			key := p.Value.In + ":" + p.Value.Name
			seen[key] = true
			result = append(result, p)
		}
	}
	// Path-item params (lower precedence)
	for _, p := range pathItem.Parameters {
		if p.Value != nil {
			key := p.Value.In + ":" + p.Value.Name
			if !seen[key] {
				result = append(result, p)
			}
		}
	}
	return result
}

// requestBodySchema returns the JSON object schema from the request body, or nil.
func requestBodySchema(op *openapi3.Operation, contentType string) *openapi3.Schema {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	ct := contentType
	if ct == "" {
		ct = "application/json"
	}
	mt := op.RequestBody.Value.Content.Get(ct)
	if mt == nil || mt.Schema == nil || mt.Schema.Value == nil {
		return nil
	}
	return mt.Schema.Value
}

// schemaToType maps an OpenAPI schema to a CLI flag type string.
func schemaToType(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil {
		return "string"
	}
	s := ref.Value

	// Handle anyOf (e.g., anyOf: [{type: string}, {type: integer}])
	if len(s.AnyOf) > 0 {
		for _, a := range s.AnyOf {
			if a.Value != nil {
				t := singleTypeToFlag(a.Value)
				if t != "" {
					return t
				}
			}
		}
		return "string"
	}

	t := singleTypeToFlag(s)
	if t != "" {
		return t
	}
	return "string"
}

// singleTypeToFlag maps a single-type schema to a flag type.
func singleTypeToFlag(s *openapi3.Schema) string {
	types := s.Type
	if types == nil {
		return ""
	}
	for _, t := range types.Slice() {
		switch t {
		case "string":
			return "string"
		case "integer":
			return "int"
		case "number":
			return "float"
		case "boolean":
			return "bool"
		case "array":
			return "stringSlice"
		}
	}
	return ""
}

// schemaEnum extracts enum values as strings.
func schemaEnum(s *openapi3.Schema) []string {
	if len(s.Enum) == 0 {
		return nil
	}
	var result []string
	for _, v := range s.Enum {
		result = append(result, fmt.Sprintf("%v", v))
	}
	return result
}

// isComplexField returns true if the schema represents a non-scalar type
// (object, array of objects, nested structures) that should be covered by --json-body.
func isComplexField(s *openapi3.Schema) bool {
	if s.Type == nil {
		// No explicit type — might be anyOf/oneOf with complex variants
		return len(s.AnyOf) > 0 || len(s.OneOf) > 0 || len(s.AllOf) > 0
	}
	types := s.Type.Slice()
	for _, t := range types {
		if t == "object" {
			return true
		}
		if t == "array" {
			// Array of scalars is fine (stringSlice); array of objects is complex
			if s.Items != nil && s.Items.Value != nil {
				itemTypes := s.Items.Value.Type
				if itemTypes != nil {
					for _, it := range itemTypes.Slice() {
						if it == "object" {
							return true
						}
					}
				}
			}
			return false
		}
	}
	return false
}

// detectPagination inspects the success response schema to detect pagination
// patterns. Returns (hasMore, tokenField, dataField).
func detectPagination(op *openapi3.Operation, doc *openapi3.T) (bool, string, string) {
	respSchema := successResponseSchema(op)
	if respSchema == nil {
		return false, "", ""
	}

	// Look for a "data" wrapper
	dataField := ""
	innerSchema := respSchema
	if dataProp := respSchema.Properties["data"]; dataProp != nil && dataProp.Value != nil {
		dataField = "data"
		innerSchema = dataProp.Value
	}

	// Check for pagination token field
	hasMore := false
	tokenField := ""
	for _, candidate := range []string{"token", "next_token", "cursor"} {
		if prop := innerSchema.Properties[candidate]; prop != nil {
			tokenField = candidate
			hasMore = true
			break
		}
	}

	return hasMore, tokenField, dataField
}

// successResponseSchema returns the schema for the 200 or 201 response.
func successResponseSchema(op *openapi3.Operation) *openapi3.Schema {
	if op.Responses == nil {
		return nil
	}
	for _, code := range []string{"200", "201"} {
		resp := op.Responses.Value(code)
		if resp == nil || resp.Value == nil {
			continue
		}
		ct := resp.Value.Content.Get("application/json")
		if ct == nil || ct.Schema == nil || ct.Schema.Value == nil {
			continue
		}
		return ct.Schema.Value
	}
	return nil
}

// floatPtrToIntPtr converts *float64 to *int.
func floatPtrToIntPtr(f *float64) *int {
	if f == nil {
		return nil
	}
	v := int(*f)
	return &v
}

// sortedPropertyNames returns property names in sorted order.
func sortedPropertyNames(s *openapi3.Schema) []string {
	var names []string
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// --- Name derivation helpers (unchanged from original) ---

func deriveGroupName(tag string, overrides *Overrides) string {
	// Check overrides first
	if name, ok := overrides.Groups[tag]; ok {
		return name
	}

	// Default: lowercase, singularize
	name := strings.ToLower(tag)
	name = singularize(name)
	return name
}

func singularize(s string) string {
	// Simple singularization: remove trailing 's' for common cases
	if strings.HasSuffix(s, "ies") {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "ses") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") {
		return s[:len(s)-1]
	}
	return s
}

// deriveSubcommandNameFromPath derives a subcommand name from the HTTP path, method,
// and content type. This replaces deriveSubcommandName that took a ParsedEndpoint.
func deriveSubcommandNameFromPath(path, method, contentType, groupName string) string {
	// Remove version prefix
	trimmed := path
	for _, prefix := range []string{"/v3/", "/v2/", "/v1/"} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}

	// Split into segments
	segments := strings.Split(trimmed, "/")

	// Get group's base path segments
	baseParts := groupToBaseParts(groupName)

	// Strip the base segments from the path.
	// Try full base first, then progressively shorter prefixes.
	remaining := segments
	for stripLen := len(baseParts); stripLen > 0; stripLen-- {
		if len(remaining) >= stripLen {
			match := true
			for i := 0; i < stripLen; i++ {
				if remaining[i] != baseParts[i] {
					match = false
					break
				}
			}
			if match {
				remaining = remaining[stripLen:]
				break
			}
		}
	}

	// Classify the remaining segments into a pattern.
	type segment struct {
		value   string
		isParam bool
	}
	var segs []segment
	for _, s := range remaining {
		segs = append(segs, segment{
			value:   strings.Trim(s, "{}"),
			isParam: strings.HasPrefix(s, "{"),
		})
	}

	// Pattern: no remaining segments → base CRUD (collection-level)
	if len(segs) == 0 {
		switch method {
		case "GET":
			return "list"
		case "POST":
			// Multipart upload endpoints use "upload" instead of "create"
			if contentType == "multipart/form-data" {
				return "upload"
			}
			return "create"
		default:
			return methodToVerb(method)
		}
	}

	// Pattern: only a param → base resource by ID (item-level)
	if len(segs) == 1 && segs[0].isParam {
		return methodToVerb(method)
	}

	// Pattern: single literal (no params after base) → sub-resource or action
	if len(segs) == 1 && !segs[0].isParam {
		name := segs[0].value
		singular := singularize(name)

		if isLikelyCollection(name) {
			switch method {
			case "GET":
				return singular + "-list"
			case "POST":
				return singular + "-create"
			}
		}
		return name
	}

	// Pattern: param followed by literal → action on a resource
	if len(segs) == 2 && segs[0].isParam && !segs[1].isParam {
		return segs[1].value
	}

	// Pattern: literal + param → sub-resource get
	if len(segs) == 2 && !segs[0].isParam && segs[1].isParam {
		singular := singularize(segs[0].value)
		return singular + "-" + methodToVerb(method)
	}

	// Pattern: literal, param, literal → sub-resource action
	if len(segs) == 3 && !segs[0].isParam && segs[1].isParam && !segs[2].isParam {
		subResource := singularize(segs[0].value)
		action := segs[2].value

		switch method {
		case "GET":
			if action == singularize(action) {
				return subResource + "-" + action + "-get"
			}
			return subResource + "-" + action
		case "PUT":
			return subResource + "-" + action + "-upload"
		case "POST":
			return subResource + "-" + singularize(action)
		default:
			return subResource + "-" + action
		}
	}

	// Fallback: join all non-param segments with hyphens
	var parts []string
	for _, s := range segs {
		if !s.isParam {
			parts = append(parts, singularize(s.value))
		}
	}
	return strings.Join(parts, "-")
}

// methodToVerb maps an HTTP method to a default CRUD verb.
func methodToVerb(method string) string {
	switch method {
	case "GET":
		return "get"
	case "POST":
		return "create"
	case "PUT":
		return "update"
	case "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

// isLikelyCollection returns true if the segment name looks like a plural
// collection rather than a singular action.
func isLikelyCollection(name string) bool {
	nonCollections := map[string]bool{
		"resources": false,
		"events":    false,
	}
	if _, ok := nonCollections[name]; ok {
		return false
	}

	collections := map[string]bool{
		"looks":      true,
		"sessions":   true,
		"proofreads": true,
		"styles":     true,
	}
	if v, ok := collections[name]; ok {
		return v
	}

	return false
}

// groupToBaseParts returns the expected base path segments for a group.
func groupToBaseParts(groupName string) []string {
	switch groupName {
	case "video":
		return []string{"videos"}
	case "avatar":
		return []string{"avatars"}
	case "voice":
		return []string{"voices"}
	case "video-agent":
		return []string{"video-agents"}
	case "translate":
		return []string{"video-translations"}
	case "webhook":
		return []string{"webhooks", "endpoints"}
	case "asset":
		return []string{"assets"}
	case "user":
		return []string{"user"}
	default:
		return []string{groupName + "s"}
	}
}

// toPascalCase converts "foo-bar" or "foo_bar" to "FooBar".
func toPascalCase(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	parts := strings.Fields(s)
	var result strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		result.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			result.WriteString(p[1:])
		}
	}
	return result.String()
}

// reservedFlags are flag names used by the builder framework.
// Generated flags must not collide with these.
var reservedFlags = map[string]bool{
	"data":    true, // -d/--data for raw JSON request body
	"d":       true,
	"help":    true, // Cobra built-in
	"h":       true,
	"version": true, // Cobra built-in
	"v":       true,
}

// safeFlagName returns the flag name, prefixed if it collides with a reserved name.
func safeFlagName(name string) string {
	if reservedFlags[name] {
		return "field-" + name
	}
	return name
}

// toKebabCase converts "snake_case" or "camelCase" to "kebab-case".
func toKebabCase(s string) string {
	s = strings.ReplaceAll(s, "_", "-")
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('-')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func formatDefault(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
