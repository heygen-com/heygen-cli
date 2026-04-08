package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"golang.org/x/term"
)

// stdinIsTerminalFunc checks whether the process stdin is a terminal.
// Tests override this to exercise the interactive confirmation path.
var stdinIsTerminalFunc = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func newCanceledError() *clierrors.CLIError {
	return &clierrors.CLIError{
		Code:     "canceled",
		Message:  "operation canceled",
		ExitCode: clierrors.ExitGeneral,
	}
}

func confirmAction(stderr io.Writer, stdin io.Reader, prompt string) error {
	_, _ = fmt.Fprintf(stderr, "%s [y/N] ", prompt)

	var answer string
	_, _ = fmt.Fscanln(stdin, &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return newCanceledError()
	}
	return nil
}
