package command

import (
	"fmt"
	"net/url"
	"strconv"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

// Spec is the generated, immutable definition of a CLI command.
// Codegen produces these from the OpenAPI spec. The builder converts
// them into Cobra commands; the executor reads the HTTP identity and
// behavioral fields when executing a request.
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
	BodyEncoding string // "json", "multipart", or "" (no body). Builder adds --json-body when "json".

	// Execution behavior (used by executor)
	TokenField  string      // non-empty → paginated; response field with next cursor
	DataField   string      // response field containing the result array (e.g., "data")
	PollConfig  *PollConfig // non-nil → pollable; defines polling behavior for --wait
	Destructive bool        // triggers --force confirmation prompt (future)
	Columns     []Column    // TUI table column definitions (future)
}

// ArgSpec defines a positional argument. Target controls where the
// resolved value is routed in the Invocation.
type ArgSpec struct {
	Name   string // display name, kebab-case ("video-id")
	Target string // "path", "body", or "file"
	Param  string // target key: path template var ("video_id") or body field name ("prompt")
	Help   string
}

// FlagSpec defines a CLI flag. Source controls whether the resolved
// value becomes a query parameter or a body field.
type FlagSpec struct {
	Name     string   // kebab-case ("folder-id")
	Type     string   // "string", "int", "bool", "float64", "string-slice"
	Default  string   // default value as string
	Help     string   // from OpenAPI description
	Required bool     // from OpenAPI required
	Enum     []string // from OpenAPI enum (empty = any value)
	Min      *int     // from OpenAPI minimum (nil if not defined)
	Max      *int     // from OpenAPI maximum (nil if not defined)
	Source   string   // "query" or "body"
	JSONName string   // original API parameter/field name ("folder_id")
}

// PollConfig defines how --wait polling works for async commands.
type PollConfig struct {
	StatusEndpoint string   // GET endpoint to check status
	StatusField    string   // JSON field containing status (e.g., "status")
	TerminalOK     []string // success states: ["completed"]
	TerminalFail   []string // failure states: ["failed", "error"]
	IDField        string   // field in create response containing the resource ID
}

// Column defines a TUI table column for --human output.
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
	Body        map[string]any    // merged from flags + --json-body; nil means no body sent
	FilePath    string            // local file path for multipart upload
}

// BuildInvocation resolves positional args and flags from a Cobra command
// into an Invocation. The merge order is:
//  1. --json-body (base, if provided)
//  2. Positional body args overlay
//  3. Flag body values overlay (flag wins over --json-body)
func (s *Spec) BuildInvocation(cmd *cobra.Command, args []string, jsonBody map[string]any) (*Invocation, error) {
	inv := &Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
	}

	// Step 1: --json-body as base (if provided)
	if jsonBody != nil {
		inv.Body = jsonBody
	}

	// Step 2: Positional args — routed by ArgSpec.Target
	for i, arg := range s.Args {
		if i >= len(args) {
			break
		}
		switch arg.Target {
		case "path":
			inv.PathParams[arg.Param] = args[i]
		case "body":
			if inv.Body == nil {
				inv.Body = make(map[string]any)
			}
			inv.Body[arg.Param] = args[i]
		case "file":
			inv.FilePath = args[i]
		}
	}

	// Step 3: Flags — only if explicitly set by the user
	for _, f := range s.Flags {
		if !cmd.Flags().Changed(f.Name) {
			continue
		}

		// Validate enum
		if len(f.Enum) > 0 {
			val, _ := cmd.Flags().GetString(f.Name)
			if !containsString(f.Enum, val) {
				return nil, clierrors.NewUsage(
					fmt.Sprintf("--%s must be one of %v, got %q", f.Name, f.Enum, val))
			}
		}

		// Validate min/max for int flags
		if f.Type == "int" && (f.Min != nil || f.Max != nil) {
			val, _ := cmd.Flags().GetInt(f.Name)
			if f.Min != nil && val < *f.Min {
				return nil, clierrors.NewUsage(
					fmt.Sprintf("--%s must be at least %d, got %d", f.Name, *f.Min, val))
			}
			if f.Max != nil && val > *f.Max {
				return nil, clierrors.NewUsage(
					fmt.Sprintf("--%s must be at most %d, got %d", f.Name, *f.Max, val))
			}
		}

		switch f.Source {
		case "query":
			inv.QueryParams.Add(f.JSONName, getFlagAsString(cmd, f))
		case "body":
			if inv.Body == nil {
				inv.Body = make(map[string]any)
			}
			inv.Body[f.JSONName] = getFlagValue(cmd, f)
		}
	}

	return inv, nil
}

// getFlagAsString reads a flag value as a string for query params.
func getFlagAsString(cmd *cobra.Command, f FlagSpec) string {
	switch f.Type {
	case "int":
		v, _ := cmd.Flags().GetInt(f.Name)
		return strconv.Itoa(v)
	case "bool":
		v, _ := cmd.Flags().GetBool(f.Name)
		return strconv.FormatBool(v)
	case "float64":
		v, _ := cmd.Flags().GetFloat64(f.Name)
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		v, _ := cmd.Flags().GetString(f.Name)
		return v
	}
}

// getFlagValue reads a flag value with its proper Go type for body fields.
func getFlagValue(cmd *cobra.Command, f FlagSpec) any {
	switch f.Type {
	case "int":
		v, _ := cmd.Flags().GetInt(f.Name)
		return v
	case "bool":
		v, _ := cmd.Flags().GetBool(f.Name)
		return v
	case "float64":
		v, _ := cmd.Flags().GetFloat64(f.Name)
		return v
	case "string-slice":
		v, _ := cmd.Flags().GetStringSlice(f.Name)
		return v
	default:
		v, _ := cmd.Flags().GetString(f.Name)
		return v
	}
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
