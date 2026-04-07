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
	ctx := &cmdContext{formatter: formatter, version: version}

	root := &cobra.Command{
		Use:   "heygen",
		Short: "HeyGen CLI — create and manage videos, avatars, and more",
		// NOTE: Only user-facing env vars are listed here. Internal/operational
		// vars (HEYGEN_MAX_RETRIES, HEYGEN_NO_ANALYTICS, HEYGEN_CONFIG_DIR) are
		// intentionally hidden. Use "heygen config list" to see all settings.
		Long: `HeyGen CLI — create and manage videos, avatars, and more.

Environment Variables:
  HEYGEN_API_KEY     API key for authentication (overrides stored credentials)
  HEYGEN_OUTPUT      Output format: json, human (default: json)

Use "heygen config list" to see all configuration settings and their sources.

Exit Codes:
  0   Success
  1   General error (API error, network failure)
  2   Usage error (invalid flags, missing arguments)
  3   Authentication error (missing or invalid API key)
  4   Timeout (resource created but operation not yet complete)

Use "heygen update" to check for and install newer versions.`,
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
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().Bool("human", false, "Display output as a formatted table instead of JSON")

	root.AddCommand(newAuthCmd(ctx))
	root.AddCommand(newConfigCmd(ctx))
	root.AddCommand(newUpdateCmd(ctx))
	registerGroups(root, ctx, gen.Groups)
	attachCustomCommands(root, ctx)
	installFlattenedHelp(root)

	return root
}

// newRootCmdWithSpecs creates a root command that registers generated commands
// from Specs instead of hand-written command constructors. Used by tests to
// verify the generic builder produces correct behavior.
func newRootCmdWithSpecs(version string, formatter output.Formatter, groups map[string][]*command.Spec) *cobra.Command {
	ctx := &cmdContext{formatter: formatter, version: version}

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
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().Bool("human", false, "Display output as a formatted table instead of JSON")

	root.AddCommand(newAuthCmd(ctx))
	root.AddCommand(newConfigCmd(ctx))
	root.AddCommand(newUpdateCmd(ctx))
	registerGroups(root, ctx, groups)
	attachCustomCommands(root, ctx)
	installFlattenedHelp(root)

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

func attachCustomCommands(root *cobra.Command, ctx *cmdContext) {
	if videoGroup := findGroup(root, "video"); videoGroup != nil {
		videoGroup.AddCommand(newVideoDownloadCmd(ctx))
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

func findGroup(root *cobra.Command, name string) *cobra.Command {
	for _, child := range root.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
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
