package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// JSONFormatter implements Formatter with machine-readable JSON output.
// Successful responses are compact JSON to out (stdout); errors are
// rendered as compact {"error": {...}} envelopes to errOut (stderr).
// Pipe through jq for pretty-printing.
type JSONFormatter struct {
	out    io.Writer
	errOut io.Writer
}

// NewJSONFormatter creates a JSONFormatter with the given writers.
func NewJSONFormatter(out, errOut io.Writer) *JSONFormatter {
	return &JSONFormatter{out: out, errOut: errOut}
}

// DefaultJSONFormatter creates a JSONFormatter using os.Stdout and os.Stderr.
func DefaultJSONFormatter() *JSONFormatter {
	return &JSONFormatter{out: os.Stdout, errOut: os.Stderr}
}

// Data writes compact JSON to stdout. Returns an error if the
// response is not valid JSON — the CLI's contract is structured output.
func (f *JSONFormatter) Data(v json.RawMessage, _ string, _ []command.Column) error {
	var parsed json.RawMessage
	if err := json.Unmarshal(v, &parsed); err != nil {
		return fmt.Errorf("API returned invalid JSON: %w", err)
	}

	compact, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}

	if _, err := f.out.Write(compact); err != nil {
		return err
	}
	_, err = f.out.Write([]byte("\n"))
	return err
}

// Error writes a CLIError as a JSON envelope to stderr.
func (f *JSONFormatter) Error(err *clierrors.CLIError) {
	envelope := err.ToErrorEnvelope()
	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		_, _ = f.errOut.Write([]byte(err.Error() + "\n"))
		return
	}
	_, _ = f.errOut.Write(data)
	_, _ = f.errOut.Write([]byte("\n"))
}
