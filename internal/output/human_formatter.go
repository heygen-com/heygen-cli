package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

var (
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	failureStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// HumanFormatter renders styled terminal-friendly output for humans.
type HumanFormatter struct {
	out    io.Writer
	errOut io.Writer
}

// NewHumanFormatter creates a HumanFormatter with the given writers.
func NewHumanFormatter(out, errOut io.Writer) *HumanFormatter {
	return &HumanFormatter{out: out, errOut: errOut}
}

// Data renders the response either as a table or as key-value output.
func (f *HumanFormatter) Data(v json.RawMessage, dataField string, columns []command.Column) error {
	payload := unwrapPayload(v, dataField)

	var rows []map[string]any
	if err := json.Unmarshal(payload, &rows); err == nil {
		return f.renderTable(rows, columns)
	}

	var primitives []any
	if err := json.Unmarshal(payload, &primitives); err == nil {
		return f.renderPrimitiveList(primitives)
	}

	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return fmt.Errorf("API returned unsupported JSON shape: %w", err)
	}
	return f.renderKeyValue(obj)
}

// Error renders a human-readable error to stderr.
func (f *HumanFormatter) Error(err *clierrors.CLIError) {
	_, _ = fmt.Fprintf(f.errOut, "%s %s\n", errorStyle.Render("Error:"), err.Message)
	if err.Hint != "" {
		_, _ = fmt.Fprintf(f.errOut, "%s %s\n", hintStyle.Render("Hint:"), err.Hint)
	}
}

func unwrapPayload(v json.RawMessage, dataField string) json.RawMessage {
	if dataField == "" {
		return v
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(v, &envelope); err != nil {
		return v
	}
	if payload, ok := envelope[dataField]; ok {
		return payload
	}
	return v
}

func (f *HumanFormatter) renderTable(rows []map[string]any, columns []command.Column) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(f.out, "No results.")
		return err
	}

	autoColumns := len(columns) == 0
	if autoColumns {
		columns = autoColumnsFor(rows[0])
	}

	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = lipgloss.Width(col.Header)
		if col.Width > widths[i] {
			widths[i] = col.Width
		}
	}

	for _, row := range rows {
		for i, col := range columns {
			cell := formatTableCell(fieldValue(row, col.Field), col.Field)
			if w := lipgloss.Width(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}

	if _, err := fmt.Fprintln(f.out, renderTableLine(headersFor(columns), widths, false)); err != nil {
		return err
	}
	for _, row := range rows {
		values := make([]string, len(columns))
		for i, col := range columns {
			values[i] = formatTableCell(fieldValue(row, col.Field), col.Field)
		}
		if _, err := fmt.Fprintln(f.out, renderTableLine(values, widths, true)); err != nil {
			return err
		}
	}

	if !autoColumns {
		totalFields := len(rows[0])
		if totalFields > len(columns) {
			_, err := fmt.Fprintf(f.out,
				"Showing %d of %d columns. Remove --human for full JSON output.\n",
				len(columns), totalFields)
			return err
		}
	}

	return nil
}

func (f *HumanFormatter) renderPrimitiveList(items []any) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(f.out, "No results.")
		return err
	}

	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{"value": item})
	}
	return f.renderTable(rows, []command.Column{{Header: "Value", Field: "value"}})
}

func (f *HumanFormatter) renderKeyValue(obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	maxLabelWidth := 0
	for key := range obj {
		keys = append(keys, key)
		label := humanizeKey(key)
		if w := lipgloss.Width(label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		label := humanizeKey(key)
		padding := strings.Repeat(" ", max(0, maxLabelWidth-lipgloss.Width(label)))
		if _, err := fmt.Fprintf(f.out, "%s:%s  %s\n", label, padding, formatCell(obj[key], key)); err != nil {
			return err
		}
	}
	return nil
}

func autoColumnsFor(row map[string]any) []command.Column {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	columns := make([]command.Column, 0, len(keys))
	for _, key := range keys {
		columns = append(columns, command.Column{
			Header: humanizeKey(key),
			Field:  key,
		})
	}
	return columns
}

func headersFor(columns []command.Column) []string {
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.Header
	}
	return headers
}

func renderTableLine(values []string, widths []int, colorize bool) string {
	cells := make([]string, len(values))
	for i, value := range values {
		padded := value + strings.Repeat(" ", max(0, widths[i]-lipgloss.Width(value)))
		if colorize {
			cells[i] = colorizeValue(value, padded)
		} else {
			cells[i] = padded
		}
	}
	return strings.Join(cells, "  ")
}

func colorizeValue(raw, padded string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "completed", "active", "enabled", "success":
		return successStyle.Render(padded)
	case "processing", "pending", "in_progress":
		return warningStyle.Render(padded)
	case "failed", "error", "disabled":
		return failureStyle.Render(padded)
	default:
		return padded
	}
}

func fieldValue(row map[string]any, path string) any {
	current := any(row)
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = obj[part]
		if !ok {
			return nil
		}
	}
	return current
}

func formatCell(v any, fieldName string) string {
	if value, ok := v.(float64); ok {
		if isUnixTimestamp(value) {
			return time.Unix(int64(value), 0).UTC().Format("2006-01-02 15:04")
		}
		if strings.Contains(strings.ToLower(fieldName), "duration") {
			return formatDuration(value)
		}
	}
	return formatValue(v)
}

func formatTableCell(v any, fieldName string) string {
	return sanitizeForTable(formatCell(v, fieldName))
}

func formatValue(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	case []any, map[string]any:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(data)
	default:
		data, err := json.Marshal(value)
		if err == nil {
			var buf bytes.Buffer
			if err := json.Compact(&buf, data); err == nil {
				return buf.String()
			}
			return string(data)
		}
		return fmt.Sprint(value)
	}
}

func isUnixTimestamp(value float64) bool {
	if value != float64(int64(value)) {
		return false
	}
	return value > 1.5e9 && value < 2.2e9
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm %ds", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

func sanitizeForTable(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(s)
}

func humanizeKey(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
