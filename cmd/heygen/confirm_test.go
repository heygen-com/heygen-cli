package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestConfirmAction_Yes(t *testing.T) {
	var stderr bytes.Buffer

	if err := confirmAction(&stderr, strings.NewReader("y\n"), "Delete this?"); err != nil {
		t.Fatalf("confirmAction returned error: %v", err)
	}
	if !strings.Contains(stderr.String(), "Delete this? [y/N]") {
		t.Fatalf("stderr = %q, want prompt", stderr.String())
	}
}

func TestConfirmAction_No(t *testing.T) {
	var stderr bytes.Buffer

	err := confirmAction(&stderr, strings.NewReader("n\n"), "Delete this?")

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "canceled" {
		t.Fatalf("err = %v, want canceled CLIError", err)
	}
}

func TestConfirmAction_EmptyDefaultsNo(t *testing.T) {
	var stderr bytes.Buffer

	err := confirmAction(&stderr, strings.NewReader("\n"), "Delete this?")

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "canceled" {
		t.Fatalf("err = %v, want canceled CLIError", err)
	}
}
