package command

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

// Groups maps group name → list of command specs in that group.
// This is the output of the codegen grouper and the input to the
// renderer and the runtime command builder.
//
//	groups["video"] = []*Spec{videoList, videoGet, videoCreate, videoDelete}
type Groups map[string][]*Spec

// SortedNames returns group names in alphabetical order for deterministic output.
func (g Groups) SortedNames() []string {
	names := make([]string, 0, len(g))
	for name := range g {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Spec is the generated, immutable definition of a CLI command.
// Codegen produces these from the OpenAPI spec. The builder converts
// them into Cobra commands; the executor reads the HTTP identity and
// behavioral fields when executing a request.
//
// Example (generated):
//
//	var VideoList = &command.Spec{
//	    Group: "video", Name: "list", Summary: "List videos",
//	    Endpoint: "/v3/videos", Method: "GET",
//	    Paginated: true,
//	    Flags: []command.FlagSpec{{Name: "limit", Type: "int", Source: "query", JSONName: "limit"}},
//	}
type Spec struct {
	// CLI presentation (used by builder, ignored by executor)
	Group       string     // parent command group ("video")
	Name        string     // subcommand name ("list")
	Summary     string     // cobra.Command.Short
	Description string     // cobra.Command.Long
	Args        []ArgSpec  // positional arguments
	Flags       []FlagSpec // CLI flags (from query params + body fields)
	Examples    []string   // usage examples shown in --help; mandatory for every command

	// HTTP identity (used by executor)
	Endpoint     string // "/v3/videos/{video_id}" — template with placeholders
	Method       string // "GET", "POST", "PUT", "PATCH", "DELETE"
	BodyEncoding string // "json", "multipart", or "" (no body). Builder adds -d/--data when "json".

	// Execution behavior (used by executor)
	Paginated  bool        // true → supports cursor pagination and the builder adds --all
	PollConfig *PollConfig // non-nil → pollable; defines polling behavior for --wait (future)
	Destructive bool        // triggers --force confirmation prompt (future)
	Columns     []Column    // TUI table column definitions (future)
}

// ArgSpec defines a positional argument derived from a URL path parameter.
// Every positional arg maps to a path template variable for URL substitution.
//
// Example: heygen video get <video-id> → PathParams["video_id"] = "abc123"
//
// Body fields and file paths are always flags (--flag), never positional.
// This is an agent-first design — named flags are self-documenting.
type ArgSpec struct {
	Name  string // display name, kebab-case ("video-id")
	Param string // path template variable ("video_id")
	Help  string
}

// FlagSpec defines a named CLI flag (--name value). Source determines
// where the resolved value is routed:
//
//   - "query": → inv.QueryParams     (e.g., --limit 10)
//   - "body":  → inv.Body            (e.g., --title "Hello")
//   - "file":  → inv.FilePath        (e.g., --file ./video.mp4, for multipart upload)
type FlagSpec struct {
	Name     string   // kebab-case ("folder-id")
	Type     string   // "string", "int", "bool", "float64", "string-slice"
	Default  string   // default value as string
	Help     string   // from OpenAPI description
	Required bool     // from OpenAPI required
	Enum     []string // from OpenAPI enum (empty = any value)
	Min      *int     // from OpenAPI minimum (nil if not defined)
	Max      *int     // from OpenAPI maximum (nil if not defined)
	Source   string   // "query", "body", or "file"
	JSONName string   // original API parameter/field name ("folder_id")
}

// PollConfig defines how --wait polling works for async commands (future).
// Will be implemented alongside Track B (polling framework).
type PollConfig struct {
	StatusEndpoint string   // GET endpoint to check status
	StatusField    string   // JSON field containing status (e.g., "status")
	TerminalOK     []string // success states: ["completed"]
	TerminalFail   []string // failure states: ["failed", "error"]
	IDField        string   // field in create response containing the resource ID
}

// Column defines a TUI table column for --human output (future).
// Will be implemented alongside M3 (TUI formatting).
type Column struct {
	Header string // table column header ("Status")
	Field  string // JSON field path, supports dot notation ("avatar.name")
	Width  int    // optional fixed width (0 = auto-size)
}

// Invocation holds the per-invocation resolved values — what the user
// actually provided. Built fresh by the builder each time a command runs.
type Invocation struct {
	PathParams  map[string]string // resolved path parameters
	QueryParams url.Values        // resolved query parameters (stdlib type, handles repeated keys)
	Body        map[string]any    // merged from flags + -d/--data; nil means no body sent
	FilePath    string            // local file path for multipart upload
}

// BuildInvocation resolves positional args and flags from a Cobra command
// into an Invocation. The merge order is:
//  1. -d/--data (base, if provided)
//  2. Positional body args overlay
//  3. Flag body values overlay (flag wins over -d/--data)
//
// -d/--data is the escape hatch for complex request bodies that can't be
// expressed as CLI flags (discriminated unions, nested objects, arrays of
// objects). It accepts inline JSON, a file path, or stdin. When used with
// individual flags, the flags overlay specific fields on top of the JSON base.
// This enables reusable JSON templates with per-invocation flag tweaks.
func (s *Spec) BuildInvocation(cmd *cobra.Command, args []string, data map[string]any) (*Invocation, error) {
	inv := &Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
	}

	// Step 1: -d/--data as base (if provided)
	if data != nil {
		inv.Body = data
	}

	// Step 2: Positional args → path params
	for i, arg := range s.Args {
		if i >= len(args) {
			break
		}
		inv.PathParams[arg.Param] = args[i]
	}

	// Step 3: Flags — only if explicitly set by the user
	for _, flag := range s.Flags {
		if !cmd.Flags().Changed(flag.Name) {
			continue
		}

		if err := validateFlag(cmd, flag); err != nil {
			return nil, err
		}

		switch flag.Source {
		case "query":
			inv.QueryParams.Add(flag.JSONName, getFlagAsString(cmd, flag))
		case "body":
			if inv.Body == nil {
				inv.Body = make(map[string]any)
			}
			inv.Body[flag.JSONName] = getFlagValue(cmd, flag)
		case "file":
			v, _ := cmd.Flags().GetString(flag.Name)
			inv.FilePath = v
		}
	}

	return inv, nil
}

// validateFlag checks enum membership and min/max bounds.
func validateFlag(cmd *cobra.Command, flag FlagSpec) error {
	if len(flag.Enum) > 0 {
		val, _ := cmd.Flags().GetString(flag.Name)
		if !slices.Contains(flag.Enum, val) {
			return clierrors.NewUsage(
				fmt.Sprintf("--%s must be one of %v, got %q", flag.Name, flag.Enum, val))
		}
	}

	if flag.Type == "int" && (flag.Min != nil || flag.Max != nil) {
		val, _ := cmd.Flags().GetInt(flag.Name)
		if flag.Min != nil && val < *flag.Min {
			return clierrors.NewUsage(
				fmt.Sprintf("--%s must be at least %d, got %d", flag.Name, *flag.Min, val))
		}
		if flag.Max != nil && val > *flag.Max {
			return clierrors.NewUsage(
				fmt.Sprintf("--%s must be at most %d, got %d", flag.Name, *flag.Max, val))
		}
	}

	return nil
}

// getFlagAsString reads a flag value as a string for query params.
func getFlagAsString(cmd *cobra.Command, flag FlagSpec) string {
	switch flag.Type {
	case "int":
		v, _ := cmd.Flags().GetInt(flag.Name)
		return strconv.Itoa(v)
	case "bool":
		v, _ := cmd.Flags().GetBool(flag.Name)
		return strconv.FormatBool(v)
	case "float64":
		v, _ := cmd.Flags().GetFloat64(flag.Name)
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		v, _ := cmd.Flags().GetString(flag.Name)
		return v
	}
}

// getFlagValue reads a flag value with its proper Go type for body fields.
func getFlagValue(cmd *cobra.Command, flag FlagSpec) any {
	switch flag.Type {
	case "int":
		v, _ := cmd.Flags().GetInt(flag.Name)
		return v
	case "bool":
		v, _ := cmd.Flags().GetBool(flag.Name)
		return v
	case "float64":
		v, _ := cmd.Flags().GetFloat64(flag.Name)
		return v
	case "string-slice":
		v, _ := cmd.Flags().GetStringSlice(flag.Name)
		return v
	default:
		v, _ := cmd.Flags().GetString(flag.Name)
		return v
	}
}
