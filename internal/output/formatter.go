package output

import (
	"encoding/json"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// Formatter controls how the CLI renders output. Successful data goes to
// stdout; errors go to stderr as structured JSON envelopes.
//
// JSONFormatter is the default (machine-readable). A TUIFormatter can be
// added behind the same interface to support --human tables and spinners.
type Formatter interface {
	// Data writes a successful API response to stdout.
	// dataField is the envelope field that contains the primary payload (for
	// example "data"). JSONFormatter ignores it and renders the full response.
	// HumanFormatter can use it to unwrap envelope responses before rendering.
	Data(v json.RawMessage, dataField string, columns []command.Column) error
	// Error writes a CLIError as a JSON envelope to stderr.
	Error(err *clierrors.CLIError)
}
