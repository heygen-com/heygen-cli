package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/iancoleman/strcase"
	"github.com/jinzhu/inflection"
)

// GroupEndpoints converts an OpenAPI document into CLI command definitions.
//
// The naming algorithm derives the command hierarchy from the URL path:
//
//  1. Group name from OpenAPI tag: lowercase, spaces→hyphens, singularize.
//     "Video Translate" → "video-translate", "Avatars" → "avatar"
//
//  2. Parse path segments after the version prefix and group root.
//     Literal segments become sub-groups; {param} segments become positional args.
//
//  3. Append a terminal verb from the HTTP method (GET→list/get, POST→create,
//     DELETE→delete, PATCH/PUT→update). This ensures every command ends with
//     an action — an agent can construct commands by combining resource + verb.
//
//  4. Exception: endpoints marked x-cli-action: true in the spec skip the
//     terminal verb. The last path segment IS the verb (e.g., /stop, /rotate-secret).
//     Without x-cli-action, /stop would produce "stop create" — nonsensical.
//
// Automation: group name, command name, flags, args, pagination, multipart,
// and body encoding are all derived automatically. The only manual inputs are:
//   - x-cli-visible in the spec (15 v1/v2 endpoints, set by API team)
//   - x-cli-action in the spec (3 action endpoints, set by API team)
//   - examples.yaml in the CLI repo (curated usage examples for every command)
//
// Common case — standard CRUD:
//
//	GET /v3/videos/{video_id}
//	  → group: "video" (from tag "Videos")
//	  → remaining segments: [{video_id}]
//	  → {video_id} is a param → positional arg
//	  → last segment is param → terminal verb from GET → "get"
//	  → result: heygen video get <video-id>
//
// Edge case — nested sub-resource with action:
//
//	POST /v3/webhooks/endpoints/{endpoint_id}/rotate-secret  [x-cli-action: true]
//	  → group: "webhook" (from tag "Webhooks")
//	  → remaining segments: [endpoints, {endpoint_id}, rotate-secret]
//	  → endpoints → sub-group, {endpoint_id} → arg, rotate-secret → literal
//	  → x-cli-action: true → no terminal verb appended
//	  → result: heygen webhook endpoints rotate-secret <endpoint-id>
//
// GroupDescriptions maps group name → description from the OpenAPI tag.
// Used by the builder for group command help text.
type GroupDescriptions map[string]string

var descriptionOverrides = GroupDescriptions{
	"voice":   "Create speech audio and manage voices",
	"webhook": "Create, list, and manage webhook endpoints and events",
}

// nameOverrides maps "METHOD /path" to a custom command name.
// Use this when the default naming algorithm produces conflicts
// (e.g., two POST endpoints in the same group both map to "create").
var nameOverrides = map[string]string{
	// POST /v3/video-agents/{session_id} sends a message to an existing session,
	// not "create". Without this, it conflicts with POST /v3/video-agents (create).
	"POST /v3/video-agents/{session_id}": "send",
	// GET /v3/video-agents/{session_id}/videos returns a list of videos, not a
	// single resource. The default heuristic sees {session_id} and uses "get".
	"GET /v3/video-agents/{session_id}/videos": "list",
}

func GroupEndpoints(doc *openapi3.T, examples Examples) (command.Groups, GroupDescriptions, error) {
	groups := make(command.Groups)
	descriptions := make(GroupDescriptions)

	// Collect tag descriptions from the spec's top-level tags array
	for _, tag := range doc.Tags {
		groupName := deriveGroupName(tag.Name)
		if override, ok := descriptionOverrides[groupName]; ok {
			descriptions[groupName] = override
			continue
		}
		if tag.Description != "" {
			descriptions[groupName] = tag.Description
		}
	}

	for _, path := range sortedMapKeys(doc.Paths.Map()) {
		pathItem := doc.Paths.Find(path)
		if pathItem == nil {
			continue
		}
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			op := pathItem.GetOperation(method)
			if op == nil || !isCliVisible(op) {
				continue
			}

			tag := "Other"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}

			groupName := deriveGroupName(tag)
			spec := buildSpec(path, method, op, pathItem, groupName, examples)
			groups[groupName] = append(groups[groupName], spec)
		}
	}

	// Sort commands within each group for deterministic output
	for _, specs := range groups {
		slices.SortFunc(specs, func(a, b *command.Spec) int {
			return strings.Compare(a.Name, b.Name)
		})
	}

	if err := validateCommandNames(groups); err != nil {
		return nil, nil, err
	}

	if err := validateFlagNames(groups); err != nil {
		return nil, nil, err
	}

	return groups, descriptions, nil
}

// buildSpec creates a command.Spec from an OpenAPI operation.
func buildSpec(
	path, method string,
	op *openapi3.Operation,
	pathItem *openapi3.PathItem,
	groupName string,
	examples Examples,
) *command.Spec {
	contentType := detectContentType(op)

	// Parse path segments after version prefix + group root.
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var remaining []string
	if len(segments) > 2 {
		remaining = segments[2:]
	}

	// Walk segments: literals → sub-groups, params → args
	var subGroups []string
	var args []command.ArgSpec
	for _, seg := range remaining {
		if strings.HasPrefix(seg, "{") {
			paramName := strings.Trim(seg, "{}")
			args = append(args, command.ArgSpec{
				Name:  strcase.ToKebab(paramName),
				Param: paramName,
			})
		} else {
			subGroups = append(subGroups, seg)
		}
	}

	spec := &command.Spec{
		Group:          groupName,
		Name:           deriveCommandName(path, method, subGroups, remaining, op),
		Summary:        op.Summary,
		Description:    op.Description,
		Endpoint:       path,
		Method:         method,
		Args:           args,
		Examples:       examples[method+" "+path],
		RequestSchema:  schemaJSON(bodySchemaRef(op, contentType)),
		ResponseSchema: schemaJSON(successResponseSchemaRef(op)),
	}

	// Body encoding
	switch contentType {
	case "application/json":
		spec.BodyEncoding = "json"
	case "multipart/form-data":
		spec.BodyEncoding = "multipart"
	}

	// Pagination — only sets Paginated bool. The cursor field names and data
	// field are API conventions hardcoded in the client package.
	spec.Paginated = detectPagination(op, pathItem)
	spec.Destructive = method == "DELETE"

	// Flags from query params
	for _, paramRef := range collectParams(pathItem, op) {
		param := paramRef.Value
		if param == nil || param.In != "query" {
			continue
		}
		flag := command.FlagSpec{
			Name:     strcase.ToKebab(param.Name),
			Type:     schemaToFlagType(param.Schema),
			Help:     param.Description,
			Required: param.Required,
			Source:   "query",
			JSONName: param.Name,
		}
		if param.Schema != nil && param.Schema.Value != nil {
			s := param.Schema.Value
			flag.Enum = schemaEnum(s)
			flag.Min = floatToIntPtr(s.Min)
			flag.Max = floatToIntPtr(s.Max)
			if s.Default != nil {
				flag.Default = formatDefault(s.Default)
			}
		}
		spec.Flags = append(spec.Flags, flag)
	}

	// Flags from request body
	if contentType == "multipart/form-data" {
		// Multipart: file fields → Source:"file" (routes to inv.FilePath)
		if schema := bodySchema(op, contentType); schema != nil {
			for _, name := range sortedMapKeys(schema.Properties) {
				propRef := schema.Properties[name]
				if propRef == nil || propRef.Value == nil || !isSchemaCliVisible(propRef) {
					continue
				}
				spec.Flags = append(spec.Flags, command.FlagSpec{
					Name:     strcase.ToKebab(name),
					Type:     "string",
					Help:     propRef.Value.Description,
					Required: true,
					Source:   "file",
					JSONName: name,
				})
			}
		}
	} else if schema := bodySchema(op, contentType); schema != nil {
		// JSON: complex fields skipped (covered by -d/--data)
		required := make(map[string]bool)
		for _, r := range schema.Required {
			required[r] = true
		}
		for _, name := range sortedMapKeys(schema.Properties) {
			propRef := schema.Properties[name]
			if propRef == nil || propRef.Value == nil || !isSchemaCliVisible(propRef) {
				continue
			}
			prop := propRef.Value
			if isComplexField(prop) {
				continue
			}
			flag := command.FlagSpec{
				Name:     strcase.ToKebab(name),
				Type:     schemaToFlagType(propRef),
				Help:     prop.Description,
				Required: required[name],
				Enum:     schemaEnum(prop),
				Min:      floatToIntPtr(prop.Min),
				Max:      floatToIntPtr(prop.Max),
				Source:   "body",
				JSONName: name,
			}
			if prop.Default != nil {
				flag.Default = formatDefault(prop.Default)
			}
			spec.Flags = append(spec.Flags, flag)
		}
	}

	return spec
}

// --- Naming ---

// deriveCommandName builds the command name from sub-groups + terminal verb.
// Exception: x-cli-action endpoints where the last literal IS the verb.
func deriveCommandName(path, method string, subGroups, allRemaining []string, op *openapi3.Operation) string {
	// Check for manual override first (resolves naming conflicts).
	// The override replaces only the terminal verb. Sub-groups are preserved.
	if override, ok := nameOverrides[method+" "+path]; ok {
		if len(subGroups) == 0 {
			return override
		}
		return strings.Join(subGroups, " ") + " " + override
	}

	if isCliAction(op) && len(subGroups) > 0 {
		return strings.Join(subGroups, " ")
	}

	verb := terminalVerb(method, allRemaining, op)
	if len(subGroups) == 0 {
		return verb
	}
	return strings.Join(subGroups, " ") + " " + verb
}

func terminalVerb(method string, remaining []string, op *openapi3.Operation) string {
	switch method {
	case "GET":
		hasParam := slices.ContainsFunc(remaining, func(s string) bool {
			return strings.HasPrefix(s, "{")
		})
		if hasParam {
			return "get"
		}
		// Singleton GET endpoints (e.g., GET /v3/user/me with summary "Get current user info")
		// are "get" not "list". We detect this from the summary because the URL structure
		// alone can't distinguish a singleton from a collection when there's no {param}.
		if strings.HasPrefix(strings.ToLower(op.Summary), "get ") {
			return "get"
		}
		return "list"
	case "POST":
		return "create"
	case "DELETE":
		return "delete"
	case "PATCH", "PUT":
		return "update"
	default:
		return strings.ToLower(method)
	}
}

func deriveGroupName(tag string) string {
	name := strings.ToLower(tag)
	name = strings.ReplaceAll(name, " ", "-")
	return inflection.Singular(name)
}

// --- OpenAPI helpers ---

func isCliVisible(op *openapi3.Operation) bool {
	if vis, ok := op.Extensions["x-cli-visible"]; ok {
		if b, ok := vis.(bool); ok {
			return b
		}
	}
	return true // default: visible
}

func isSchemaCliVisible(ref *openapi3.SchemaRef) bool {
	if ref == nil || ref.Value == nil {
		return true
	}
	if vis, ok := ref.Value.Extensions["x-cli-visible"]; ok {
		if b, ok := vis.(bool); ok {
			return b
		}
	}
	return true
}

func isCliAction(op *openapi3.Operation) bool {
	if v, ok := op.Extensions["x-cli-action"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func detectContentType(op *openapi3.Operation) string {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return ""
	}
	// Prefer multipart, then JSON
	for _, ct := range []string{"multipart/form-data", "application/json"} {
		if _, ok := op.RequestBody.Value.Content[ct]; ok {
			return ct
		}
	}
	for ct := range op.RequestBody.Value.Content {
		return ct
	}
	return ""
}

func collectParams(pathItem *openapi3.PathItem, op *openapi3.Operation) openapi3.Parameters {
	seen := make(map[string]bool)
	var result openapi3.Parameters
	// Operation params take precedence over path-item params
	for _, p := range op.Parameters {
		if p.Value != nil {
			seen[p.Value.In+":"+p.Value.Name] = true
			result = append(result, p)
		}
	}
	for _, p := range pathItem.Parameters {
		if p.Value != nil && !seen[p.Value.In+":"+p.Value.Name] {
			result = append(result, p)
		}
	}
	return result
}

func bodySchema(op *openapi3.Operation, contentType string) *openapi3.Schema {
	ref := bodySchemaRef(op, contentType)
	if ref == nil {
		return nil
	}
	return ref.Value
}

func bodySchemaRef(op *openapi3.Operation, contentType string) *openapi3.SchemaRef {
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
	return mt.Schema
}

// --- Schema type mapping ---

// openAPITypeToFlag maps OpenAPI type strings to CLI flag type strings.
var openAPITypeToFlag = map[string]string{
	"string":  "string",
	"integer": "int",
	"number":  "float64",
	"boolean": "bool",
	"array":   "string-slice",
}

func schemaToFlagType(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil {
		return "string"
	}
	s := ref.Value
	// Handle nullable anyOf by unwrapping to the concrete promoted type.
	if unwrapped := unwrapNullableType(s); unwrapped != nil {
		if t := mapSchemaType(unwrapped); t != "" {
			return t
		}
		return "string"
	}
	// Handle other anyOf patterns by picking the first mappable branch.
	if len(s.AnyOf) > 0 {
		for _, a := range s.AnyOf {
			if a.Value != nil {
				if t := mapSchemaType(a.Value); t != "" {
					return t
				}
			}
		}
		return "string"
	}
	if t := mapSchemaType(s); t != "" {
		return t
	}
	return "string"
}

func mapSchemaType(s *openapi3.Schema) string {
	if s.Type == nil {
		return ""
	}
	for _, t := range s.Type.Slice() {
		if flagType, ok := openAPITypeToFlag[t]; ok {
			return flagType
		}
	}
	return ""
}

func schemaEnum(s *openapi3.Schema) []string {
	if len(s.Enum) > 0 {
		return formatEnum(s.Enum)
	}
	if s.Items != nil && s.Items.Value != nil && len(s.Items.Value.Enum) > 0 {
		return formatEnum(s.Items.Value.Enum)
	}
	if unwrapped := unwrapNullableType(s); unwrapped != nil {
		if len(unwrapped.Enum) > 0 {
			return formatEnum(unwrapped.Enum)
		}
		if unwrapped.Items != nil && unwrapped.Items.Value != nil && len(unwrapped.Items.Value.Enum) > 0 {
			return formatEnum(unwrapped.Items.Value.Enum)
		}
	}
	return nil
}

func isComplexField(s *openapi3.Schema) bool {
	if s.Type == nil {
		if unwrapNullableType(s) != nil {
			return false
		}
		return len(s.AnyOf) > 0 || len(s.OneOf) > 0 || len(s.AllOf) > 0
	}
	return isComplexType(s)
}

func formatEnum(vals []any) []string {
	result := make([]string, 0, len(vals))
	for _, v := range vals {
		result = append(result, fmt.Sprintf("%v", v))
	}
	return result
}

func unwrapNullableType(s *openapi3.Schema) *openapi3.Schema {
	if len(s.AnyOf) != 2 {
		return nil
	}
	var nullCount int
	var candidate *openapi3.Schema
	for _, branch := range s.AnyOf {
		if branch == nil || branch.Value == nil {
			return nil
		}
		if isNullType(branch.Value) {
			nullCount++
			continue
		}
		candidate = branch.Value
	}
	if nullCount != 1 || candidate == nil {
		return nil
	}
	if isComplexType(candidate) {
		return nil
	}
	return candidate
}

func isNullType(s *openapi3.Schema) bool {
	if s.Type == nil {
		return false
	}
	for _, t := range s.Type.Slice() {
		if t == "null" {
			return true
		}
	}
	return false
}

func isComplexType(s *openapi3.Schema) bool {
	if s.Type == nil {
		return true
	}
	for _, t := range s.Type.Slice() {
		if t == "object" {
			return true
		}
		if t == "array" && s.Items != nil && s.Items.Value != nil {
			if s.Items.Value.Type == nil {
				return true
			}
			for _, it := range s.Items.Value.Type.Slice() {
				if it == "object" {
					return true
				}
			}
		} else if t == "array" {
			return true
		}
	}
	return false
}

// --- Response analysis ---

// detectPagination returns true if the endpoint supports cursor-based pagination.
// An endpoint is paginated when both: (1) the response has a cursor field
// (next_token) and (2) the request has a cursor query param (token).
func detectPagination(op *openapi3.Operation, pathItem *openapi3.PathItem) bool {
	respSchema := successResponseSchema(op)
	if respSchema == nil {
		return false
	}

	// Check for cursor field in response (at root or inside data wrapper).
	hasCursorField := false
	schemasToCheck := []*openapi3.Schema{respSchema}
	if dataProp := respSchema.Properties["data"]; dataProp != nil && dataProp.Value != nil {
		schemasToCheck = append(schemasToCheck, dataProp.Value)
	}
	for _, schema := range schemasToCheck {
		if _, ok := schema.Properties["next_token"]; ok {
			hasCursorField = true
			break
		}
	}
	if !hasCursorField {
		return false
	}

	return detectCursorParam(pathItem, op) != ""
}

func detectCursorParam(pathItem *openapi3.PathItem, op *openapi3.Operation) string {
	params := collectParams(pathItem, op)
	for _, paramRef := range params {
		param := paramRef.Value
		if param == nil || param.In != "query" {
			continue
		}
		if param.Name == "token" {
			return param.Name
		}
	}
	return ""
}

func successResponseSchema(op *openapi3.Operation) *openapi3.Schema {
	ref := successResponseSchemaRef(op)
	if ref == nil {
		return nil
	}
	return ref.Value
}

func successResponseSchemaRef(op *openapi3.Operation) *openapi3.SchemaRef {
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
		return ct.Schema
	}
	return nil
}

// --- Helpers ---

func floatToIntPtr(f *float64) *int {
	if f == nil {
		return nil
	}
	v := int(*f)
	return &v
}

// sortedMapKeys returns sorted keys from any map[string]V.
func sortedMapKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
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
		return fmt.Sprintf("%t", val)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// --- Validation ---

var reservedFlags = map[string]bool{
	"data": true, "d": true,
	"help": true, "h": true,
	"version": true, "v": true,
}

// validateCommandNames checks for duplicate command names within a group.
// If two endpoints produce the same name, codegen would generate duplicate
// Go variable declarations. This catches the conflict early with an actionable
// error message pointing to the nameOverrides map.
func validateCommandNames(groups command.Groups) error {
	for groupName, specs := range groups {
		seen := make(map[string]string) // name → endpoint
		for _, spec := range specs {
			if prev, ok := seen[spec.Name]; ok {
				return fmt.Errorf(
					"naming conflict in group %q: two endpoints map to %q\n"+
						"  %s\n  %s %s\n"+
						"Add an entry to nameOverrides in codegen/grouper.go to disambiguate",
					groupName, spec.Name, prev, spec.Method, spec.Endpoint)
			}
			seen[spec.Name] = spec.Method + " " + spec.Endpoint
		}
	}
	return nil
}

func validateFlagNames(groups command.Groups) error {
	for _, specs := range groups {
		for _, spec := range specs {
			for _, flag := range spec.Flags {
				if reservedFlags[flag.Name] {
					return fmt.Errorf(
						"flag %q for %s %s collides with reserved flag",
						flag.Name, spec.Method, spec.Endpoint)
				}
			}
		}
	}
	return nil
}
