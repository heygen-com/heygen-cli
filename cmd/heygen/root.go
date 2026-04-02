package main

import (
	"slices"
	"strings"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/spf13/cobra"
)

func newRootCmd(version string, formatter output.Formatter) *cobra.Command {
	ctx := &cmdContext{formatter: formatter}

	root := &cobra.Command{
		Use:   "heygen",
		Short: "HeyGen CLI — create and manage videos, avatars, and more",
		// NOTE: env var list is hardcoded. Keep in sync with envVarByKey in env_provider.go.
		Long: `HeyGen CLI — create and manage videos, avatars, and more.

Environment Variables:
  HEYGEN_API_KEY            API key for authentication (overrides stored credentials)
  HEYGEN_OUTPUT             Output format: json, human (default: json)
  HEYGEN_NO_ANALYTICS       Disable analytics when set (default: enabled)
  HEYGEN_NO_UPDATE_CHECK    Disable update check when set (default: enabled)
  HEYGEN_CONFIG_DIR         Override config directory (default: ~/.heygen)`,
		Version:       version,
		SilenceUsage:  true, // we handle usage errors ourselves
		SilenceErrors: true, // we handle error output ourselves
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initContext(cmd, version, ctx)
		},
	}

	// Map Cobra flag-parsing errors to CLIError with ExitUsage (exit code 2).
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})

	root.AddCommand(newAuthCmd(ctx))
	root.AddCommand(newConfigCmd(ctx))
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
			return initContext(cmd, version, ctx)
		},
	}

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})

	root.AddCommand(newAuthCmd(ctx))
	root.AddCommand(newConfigCmd(ctx))
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
		short := gen.GroupDescriptions[groupName]
		if short == "" {
			short = humanizeCommandToken(groupName) + " commands"
		}
		groupCmd := &cobra.Command{Use: groupName, Short: short}
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
