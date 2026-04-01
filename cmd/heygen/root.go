package main

import (
	"slices"
	"strings"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/spf13/cobra"
)

// cmdContext holds shared dependencies created in PersistentPreRunE
// and consumed by child commands via closures.
type cmdContext struct {
	client         *client.Client
	formatter      output.Formatter
	configProvider config.Provider
}

func newRootCmd(version string, formatter output.Formatter) *cobra.Command {
	ctx := &cmdContext{formatter: formatter}

	root := &cobra.Command{
		Use:           "heygen",
		Short:         "HeyGen CLI — create and manage videos, avatars, and more",
		Version:       version,
		SilenceUsage:  true, // we handle usage errors ourselves
		SilenceErrors: true, // we handle error output ourselves
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// 1. Create config provider (BaseURL only — auth is Resolver's job)
			provider := &config.EnvProvider{}
			ctx.configProvider = provider

			// 2. Resolve credentials (env var today; file-based storage later)
			resolver := &auth.EnvCredentialResolver{}
			apiKey, err := resolver.Resolve()
			if err != nil {
				return err
			}

			// 3. Create client using config.Provider for BaseURL
			ctx.client = client.New(apiKey,
				client.WithBaseURL(provider.BaseURL()),
				client.WithUserAgent("heygen-cli/"+version),
			)

			return nil
		},
	}

	// Map Cobra flag-parsing errors to CLIError with ExitUsage (exit code 2).
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})

	registerGroups(root, ctx, gen.Groups)

	return root
}

// newRootCmdWithSpecs creates a root command that registers generated commands
// from Specs instead of hand-written command constructors. Used by tests to
// verify the generic builder produces correct behavior.
func newRootCmdWithSpecs(version string, formatter output.Formatter, groups map[string][]*command.Spec) *cobra.Command {
	ctx := &cmdContext{formatter: formatter}

	root := &cobra.Command{
		Use:           "heygen",
		Short:         "HeyGen CLI — create and manage videos, avatars, and more",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			provider := &config.EnvProvider{}
			ctx.configProvider = provider

			resolver := &auth.EnvCredentialResolver{}
			apiKey, err := resolver.Resolve()
			if err != nil {
				return err
			}

			ctx.client = client.New(apiKey,
				client.WithBaseURL(provider.BaseURL()),
				client.WithUserAgent("heygen-cli/"+version),
			)

			return nil
		},
	}

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})

	registerGroups(root, ctx, groups)

	return root
}

func registerGroups(root *cobra.Command, ctx *cmdContext, groups map[string][]*command.Spec) {
	groupNames := make([]string, 0, len(groups))
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	slices.Sort(groupNames)

	for _, groupName := range groupNames {
		groupCmd := &cobra.Command{Use: groupName, Short: humanizeCommandToken(groupName) + " commands"}
		for _, spec := range groups[groupName] {
			registerSpecCommand(groupCmd, spec, ctx)
		}
		root.AddCommand(groupCmd)
	}
}

func registerSpecCommand(groupCmd *cobra.Command, spec *command.Spec, ctx *cmdContext) {
	path := commandPathParts(spec)
	if len(path) == 0 {
		groupCmd.AddCommand(buildCobraCommand(spec, ctx))
		return
	}

	parent := groupCmd
	for _, token := range path[:len(path)-1] {
		parent = ensureIntermediateCommand(parent, token)
	}

	parent.AddCommand(buildCobraCommand(spec, ctx))
}

func ensureIntermediateCommand(parent *cobra.Command, token string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == token {
			return child
		}
	}

	child := &cobra.Command{
		Use:   token,
		Short: humanizeCommandToken(token) + " commands",
	}
	parent.AddCommand(child)
	return child
}

func humanizeCommandToken(token string) string {
	parts := strings.Split(token, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
