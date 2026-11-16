package output

import (
	"fmt"
	"strings"
)

// terminalText shows C0 controls (tab and newline aside), DEL and C1 controls as visible \xNN text, so a string
// from the API cannot move the cursor, retitle the window or hide a link in the person's terminal.
func terminalText(s string) string {
	if !strings.ContainsFunc(s, isTerminalControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isTerminalControl(r) {
			fmt.Fprintf(&b, `\x%02x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isTerminalControl(r rune) bool {
	return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
