package main

import (
	"fmt"
	"sort"
	"strings"
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

// GroupEndpoints groups parsed endpoints by tag and derives command names.
func GroupEndpoints(endpoints []ParsedEndpoint, overrides *Overrides) []CommandGroup {
	// Filter out skipped endpoints
	var filtered []ParsedEndpoint
	for _, ep := range endpoints {
		if overrides.ShouldSkip(ep.Method, ep.Path) {
			continue
		}
		filtered = append(filtered, ep)
	}

	// Group by tag
	byTag := make(map[string][]ParsedEndpoint)
	for _, ep := range filtered {
		tag := ep.Tag
		if tag == "" {
			tag = "Other"
		}
		byTag[tag] = append(byTag[tag], ep)
	}

	// Build command groups
	var groups []CommandGroup
	for tag, eps := range byTag {
		groupName := deriveGroupName(tag, overrides)
		group := CommandGroup{Name: groupName}

		for _, ep := range eps {
			cmd := buildCommand(ep, groupName, overrides)
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

func buildCommand(ep ParsedEndpoint, groupName string, overrides *Overrides) GroupedCommand {
	subName := deriveSubcommandName(ep, groupName)
	key := ep.Method + " " + ep.Path

	cmd := GroupedCommand{
		Group:       groupName,
		Name:        subName,
		Summary:     ep.Summary,
		Description: ep.Description,
		Endpoint:    ep.Path,
		Method:      ep.Method,
		VarName:     toPascalCase(groupName) + toPascalCase(subName),
	}

	// Examples from overrides
	if examples, ok := overrides.Examples[key]; ok {
		cmd.Examples = examples
	}

	// Body encoding
	switch ep.ContentType {
	case "application/json":
		cmd.BodyEncoding = "json"
	case "multipart/form-data":
		cmd.BodyEncoding = "multipart"
	default:
		cmd.BodyEncoding = ""
	}

	// Pagination
	if ep.HasMore && ep.TokenField != "" {
		cmd.TokenField = ep.TokenField
	}
	cmd.DataField = ep.DataField

	// Build positional override lookup
	positionalOverrides := overrides.GetPositionalOverrides(key)
	positionalByField := make(map[string]PositionalOverride)
	for _, po := range positionalOverrides {
		positionalByField[po.Field] = po
	}

	// Path params → ArgDefs (unless overridden)
	for _, p := range ep.PathParams {
		argName := toKebabCase(p.Name)
		cmd.Args = append(cmd.Args, ArgDef{
			Name:   argName,
			Target: "path",
			Param:  p.Name,
			Help:   p.Description,
		})
	}

	// Check for positional overrides that promote body/file fields to args
	for _, po := range positionalOverrides {
		switch po.Target {
		case "body":
			// Find the body field and promote it to a positional arg
			help := ""
			for _, f := range ep.BodyFields {
				if f.Name == po.Field {
					help = f.Description
					break
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
	for _, p := range ep.QueryParams {
		// Skip 'token' params — they're for pagination, not user-facing flags
		if p.Name == "token" {
			continue
		}
		flag := FlagDef{
			Name:     toKebabCase(p.Name),
			Type:     p.Type,
			Help:     p.Description,
			Required: p.Required,
			Enum:     p.Enum,
			Min:      p.Min,
			Max:      p.Max,
			Source:   "query",
			JSONName: p.Name,
		}
		if p.Default != nil {
			flag.Default = formatDefault(p.Default)
		}
		cmd.Flags = append(cmd.Flags, flag)
	}

	// Body fields → FlagDefs (skip complex fields and positional overrides)
	for _, f := range ep.BodyFields {
		// Skip complex fields — they're covered by --json-body
		if f.Complex {
			continue
		}
		// Skip fields promoted to positional args
		if _, ok := positionalByField[f.Name]; ok {
			continue
		}
		flag := FlagDef{
			Name:     toKebabCase(f.Name),
			Type:     f.Type,
			Help:     f.Description,
			Required: f.Required,
			Enum:     f.Enum,
			Min:      f.Min,
			Max:      f.Max,
			Source:   "body",
			JSONName: f.Name,
		}
		if f.Default != nil {
			flag.Default = formatDefault(f.Default)
		}
		cmd.Flags = append(cmd.Flags, flag)
	}

	return cmd
}

func deriveSubcommandName(ep ParsedEndpoint, groupName string) string {
	// Remove version prefix
	path := ep.Path
	for _, prefix := range []string{"/v3/", "/v2/", "/v1/"} {
		path = strings.TrimPrefix(path, prefix)
	}

	// Split into segments
	segments := strings.Split(path, "/")

	// Get group's base path segments
	baseParts := groupToBaseParts(groupName)

	// Strip the base segments from the path.
	// Try full base first, then progressively shorter prefixes.
	// This handles cases like webhook where /v3/webhooks/endpoints is the
	// primary sub-path but /v3/webhooks/event-types is a sibling.
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
	// Build a structural pattern of "literal" vs "param" segments.
	// Examples:
	//   [] → base resource
	//   [{id}] → base resource with ID
	//   [looks] → sub-resource collection
	//   [looks, {id}] → sub-resource item
	//   [{id}, consent] → action on resource
	//   [sessions, {id}, messages] → nested action on sub-resource
	//   [proofreads, {id}, srt] → nested action on sub-resource
	//   [event-types] → non-CRUD collection endpoint
	//   [languages] → non-CRUD collection endpoint

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

	method := ep.Method

	// Pattern: no remaining segments → base CRUD (collection-level)
	if len(segs) == 0 {
		switch method {
		case "GET":
			return "list"
		case "POST":
			// Multipart upload endpoints use "upload" instead of "create"
			if ep.ContentType == "multipart/form-data" {
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
	// e.g., [looks], [speech], [languages], [styles], [event-types], [events], [me]
	if len(segs) == 1 && !segs[0].isParam {
		name := segs[0].value
		singular := singularize(name)

		// If it looks like a plural noun that maps to a CRUD sub-collection:
		// Apply list/create heuristics based on HTTP method
		if isLikelyCollection(name) {
			switch method {
			case "GET":
				return singular + "-list"
			case "POST":
				return singular + "-create"
			}
		}
		// Otherwise it's an action or singleton endpoint — use the name directly
		return name
	}

	// Pattern: param followed by literal → action on a resource
	// e.g., [{id}, consent], [{id}, caption]
	if len(segs) == 2 && segs[0].isParam && !segs[1].isParam {
		return segs[1].value
	}

	// Pattern: literal + param → sub-resource get
	// e.g., [looks, {id}]
	if len(segs) == 2 && !segs[0].isParam && segs[1].isParam {
		singular := singularize(segs[0].value)
		return singular + "-" + methodToVerb(method)
	}

	// Pattern: literal, param, literal → sub-resource action
	// e.g., [sessions, {id}, messages], [sessions, {id}, stop],
	//       [proofreads, {id}, srt], [proofreads, {id}, generate]
	if len(segs) == 3 && !segs[0].isParam && segs[1].isParam && !segs[2].isParam {
		subResource := singularize(segs[0].value)
		action := segs[2].value

		switch method {
		case "GET":
			// If the action is a singular noun (not already plural), add -get
			// to disambiguate from PUT/POST on the same path.
			// e.g., "srt" → "proofread-srt-get", but "resources" stays "session-resources"
			if action == singularize(action) {
				return subResource + "-" + action + "-get"
			}
			return subResource + "-" + action
		case "PUT":
			return subResource + "-" + action + "-upload"
		case "POST":
			// Singularize collection-like action names (messages → message)
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
// collection (ends with 's', 'es', etc.) rather than a singular action.
func isLikelyCollection(name string) bool {
	// Known non-collection endings that happen to end in 's'
	nonCollections := map[string]bool{
		"resources": false, // actually is a collection-like name
		"events":    false,
	}
	if _, ok := nonCollections[name]; ok {
		return false // treat as action/name
	}

	// Names that are clearly sub-resource collections
	collections := map[string]bool{
		"looks":      true,
		"sessions":   true,
		"proofreads": true,
		"styles":     true,
	}
	if v, ok := collections[name]; ok {
		return v
	}

	// Default: if it ends in 's' and is not a known action, might be a collection
	// But be conservative — most endpoints after a param are actions
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
		// Default: try the group name + "s" (pluralized) as the base path
		return []string{groupName + "s"}
	}
}

// toPascalCase converts "foo-bar" or "foo_bar" to "FooBar".
func toPascalCase(s string) string {
	// Replace hyphens and underscores with spaces for splitting
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

// toKebabCase converts "snake_case" or "camelCase" to "kebab-case".
func toKebabCase(s string) string {
	// Replace underscores with hyphens
	s = strings.ReplaceAll(s, "_", "-")
	// Insert hyphens before uppercase letters (for camelCase)
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
		// Check if it's a whole number — format as integer
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
