package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/spf13/cobra"
)

func newRootCmd(version string, formatter output.Formatter, analyticsClient *analytics.Client) *cobra.Command {
	ctx := &cmdContext{formatter: formatter, version: version}

	root := &cobra.Command{
		Use:   "heygen",
		Short: "HeyGen CLI — create and manage videos, avatars, and more",
		// NOTE: Only user-facing env vars are listed here. Internal/operational
		// vars (HEYGEN_MAX_RETRIES, HEYGEN_NO_ANALYTICS, HEYGEN_CONFIG_DIR) are
		// intentionally hidden. Use "heygen config list" to see all settings.
		Long:          `HeyGen CLI — create and manage videos, avatars, and more.`,
		Version:       version,
		SilenceUsage:  true, // we handle usage errors ourselves
		SilenceErrors: true, // we handle error output ourselves
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			analyticsClient.CommandRun(cmd.CommandPath())
			return initContext(cmd, version, ctx)
		},
	}

	// Map Cobra flag-parsing errors to CLIError with ExitUsage (exit code 2).
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().Bool("human", false, "Display output as a formatted table instead of JSON")

	// Move examples after flags in help output (matches gh, aws, gcloud convention).
	// Cobra's default puts examples before flags; most CLIs do the opposite.
	root.SetUsageTemplate(usageTemplateExamplesLast)

	root.AddCommand(newAuthCmd(ctx))
	root.AddCommand(newConfigCmd(ctx))
	root.AddCommand(newUpdateCmd(ctx))
	registerGroups(root, ctx, gen.Groups)
	attachCustomCommands(root, ctx)
	installFlattenedHelp(root)
	installRootHelpFooter(root)

	return root
}

// installRootHelpFooter appends environment variables, exit codes, and hints
// after the standard Cobra help output (commands + flags first, details last).
func installRootHelpFooter(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		if cmd != root {
			return
		}
		fmt.Fprint(cmd.OutOrStdout(), `
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

Error Envelope (stderr on failure):
  {"error": {"code": "...", "message": "...", "hint": "...", "retryable": true|false}}
  retryable is best-effort: true (transient), false (permanent), or omitted (unknown).
  Branch on retryable, not on specific code values.

Tip: Use --request-schema on any command to see the expected JSON input fields.
Use "heygen update" to check for and install newer versions.
`)
	})
}

// newRootCmdWithSpecs creates a root command that registers generated commands
// from Specs instead of hand-written command constructors. Used by tests to
// verify the generic builder produces correct behavior.
func newRootCmdWithSpecs(version string, formatter output.Formatter, analyticsClient *analytics.Client, groups map[string][]*command.Spec) *cobra.Command {
	ctx := &cmdContext{formatter: formatter, version: version}

	root := &cobra.Command{
		Use:           "heygen",
		Short:         "HeyGen CLI — create and manage videos, avatars, and more",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			analyticsClient.CommandRun(cmd.CommandPath())
			return initContext(cmd, version, ctx)
		},
	}

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return clierrors.NewUsage(err.Error())
	})
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().Bool("human", false, "Display output as a formatted table instead of JSON")

	root.SetUsageTemplate(usageTemplateExamplesLast)

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
		groupCmd := newCommandGroup(groupName, short)
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

	child := newCommandGroup(token, humanizeCommandToken(token)+" commands")
	parent.AddCommand(child)
	return child
}

func newCommandGroup(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				var available []string
				for _, sub := range cmd.Commands() {
					if !sub.Hidden && sub.Name() != "help" {
						available = append(available, sub.Name())
					}
				}
				sort.Strings(available)
				return clierrors.NewUsage(
					fmt.Sprintf("unknown command %q for %q.\nAvailable subcommands: %s",
						args[0], cmd.CommandPath(), strings.Join(available, ", ")))
			}
			return cmd.Help()
		},
	}
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

// usageTemplateExamplesLast is Cobra's default usage template with Examples
// moved after Global Flags. Matches the convention used by gh, aws, and gcloud.
const usageTemplateExamplesLast = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
