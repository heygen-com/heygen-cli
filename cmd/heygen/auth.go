package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/auth"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/paths"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "auth",
		Short:       "Manage authentication",
		Annotations: map[string]string{"skipAuth": "true"},
	}
	cmd.AddCommand(newAuthLoginCmd(ctx))
	return cmd
}

func newAuthLoginCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store API key for CLI authentication",
		Long: `Reads an API key from stdin and stores it for future CLI use.

Interactive:
  heygen auth login

Piped:
  echo "$KEY" | heygen auth login

For CI/Docker/agents, set the HEYGEN_API_KEY environment variable instead.
The env var takes priority over stored credentials.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := readAPIKey(cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if key == "" {
				return clierrors.NewUsage("no API key provided")
			}

			store := &auth.FileCredentialStore{}
			if err := store.Save(key); err != nil {
				return clierrors.New(fmt.Sprintf("failed to save credentials: %v", err))
			}

			credPath := filepath.Join(paths.ConfigDir(), "credentials")
			data, err := json.Marshal(map[string]string{
				"message": "API key saved to " + credPath,
			})
			if err != nil {
				return clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
			}

			return ctx.formatter.Data(data)
		},
	}
}

func readAPIKey(in io.Reader, errOut io.Writer) (string, error) {
	if file, ok := in.(interface{ Fd() uintptr }); ok && term.IsTerminal(int(file.Fd())) {
		if _, err := fmt.Fprint(errOut, "Enter API key: "); err != nil {
			return "", clierrors.New(fmt.Sprintf("failed to write prompt: %v", err))
		}

		raw, err := term.ReadPassword(int(file.Fd()))
		if _, writeErr := fmt.Fprintln(errOut); writeErr != nil && err == nil {
			err = writeErr
		}
		if err != nil {
			return "", clierrors.New(fmt.Sprintf("failed to read input: %v", err))
		}

		return strings.TrimSpace(string(raw)), nil
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", clierrors.New(fmt.Sprintf("failed to read stdin: %v", err))
	}
	return strings.TrimSpace(string(data)), nil
}
