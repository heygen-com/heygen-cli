package client

// RequestSpec is the contract between commands and the HTTP executor.
// A command (hand-written or auto-generated) populates a RequestSpec;
// the executor converts it to an HTTP request, handles the response,
// and returns raw JSON.
//
// The full struct is defined upfront so that codegen, pagination, polling,
// and multipart upload can rely on a stable shape.
type RequestSpec struct {
	Endpoint     string            // "/v3/videos"
	Method       string            // GET, POST, DELETE, PATCH
	PathParams   map[string]string // {video_id: "abc123"}
	QueryParams  []QueryParam      // typed query params (supports repeated keys)
	Body         []FieldSpec       // typed body fields (supports nested objects)
	BodyEncoding string            // "json" (default) or "multipart"
	FilePath     string            // local file path for multipart upload
	Paginated    bool              // enables --all accumulation
	TokenField   string            // pagination cursor field in response
	DataField    string            // response array field name
	Pollable     bool              // enables --wait
	PollConfig   *PollConfig       // status field, terminal states
	Destructive  bool              // triggers confirmation prompt (--force skips)
	Columns      []Column          // TUI table column definitions
}

// QueryParam represents a single query parameter. Repeated allows
// multiple values for the same key (e.g., ?status=a&status=b).
type QueryParam struct {
	Key      string
	Value    string
	Repeated bool
}

// FieldSpec represents a typed body field for JSON request bodies.
// The codegen pipeline emits these from OpenAPI request body schemas.
type FieldSpec struct {
	Name     string // JSON field name
	Type     string // "string", "int", "bool", "object", "array"
	Value    any    // typed value from flag
	Required bool
}

// PollConfig defines how --wait polling works for async commands.
type PollConfig struct {
	StatusEndpoint string   // GET endpoint to check status
	StatusField    string   // JSON field containing status (e.g., "status")
	TerminalOK     []string // Success states: ["completed"]
	TerminalFail   []string // Failure states: ["failed", "error"]
	IDField        string   // Field in create response with resource ID
}

// Column defines a TUI table column for --human output.
type Column struct {
	Header string // Table column header ("Status")
	Field  string // JSON field path, supports dot notation ("avatar.name")
	Width  int    // Optional fixed width (0 = auto-size)
}
