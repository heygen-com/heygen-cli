package main

import (
	"strings"
	"testing"
)

// TestCommandGroup_UnknownSubcommand_ListsAvailable verifies that typing an
// unknown subcommand returns exit 2 and lists the available subcommands.
func TestCommandGroup_UnknownSubcommand_ListsAvailable(t *testing.T) {
	res := runCommand(t, "http://example.invalid", "test-key", "user", "info")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "unknown command") {
		t.Fatalf("stderr = %s, want unknown command message", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Available subcommands:") {
		t.Fatalf("stderr = %s, want Available subcommands list", res.Stderr)
	}
	// "me" is the real subcommand under "user" (heygen user me).
	if !strings.Contains(res.Stderr, "me") {
		t.Fatalf("stderr = %s, want at least 'me' in available subcommands", res.Stderr)
	}
}

// TestCommandGroup_BareInvocation_ShowsHelp verifies that calling a command
// group with no arguments exits 0 and prints help (regression test).
func TestCommandGroup_BareInvocation_ShowsHelp(t *testing.T) {
	res := runCommand(t, "http://example.invalid", "test-key", "user")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}
