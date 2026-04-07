package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type helpEntry struct {
	Path  string
	Short string
}

func installFlattenedHelp(root *cobra.Command) {
	for _, cmd := range allCommands(root) {
		if cmd == root || !shouldFlattenHelp(cmd) {
			continue
		}

		defaultHelp := cmd.HelpFunc()
		cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
			if !shouldFlattenHelp(cmd) {
				defaultHelp(cmd, args)
				return
			}
			renderFlattenedHelp(cmd)
		})
	}
}

func allCommands(root *cobra.Command) []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(root.Commands())+1)
	for _, child := range root.Commands() {
		cmds = append(cmds, allCommands(child)...)
	}
	return append(cmds, root)
}

func shouldFlattenHelp(cmd *cobra.Command) bool {
	if cmd.Parent() == nil {
		return false
	}

	for _, child := range visibleChildren(cmd) {
		if !isLeafCommand(child) {
			return true
		}
	}
	return false
}

func renderFlattenedHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	writeHelpHeader(out, cmd)

	entries := flattenedHelpEntries(cmd)
	if len(entries) > 0 {
		writeAvailableCommands(out, entries)
	}

	writeFlagSections(out, cmd)

	if len(entries) > 0 {
		_, _ = fmt.Fprintf(out, "Use %q for more information about a command.\n", cmd.CommandPath()+" [command] --help")
	}
}

func writeHelpHeader(out io.Writer, cmd *cobra.Command) {
	description := cmd.Long
	if description == "" {
		description = cmd.Short
	}
	if description != "" {
		_, _ = fmt.Fprintln(out, description)
		_, _ = fmt.Fprintln(out)
	}

	usage := cmd.CommandPath()
	if len(visibleChildren(cmd)) > 0 {
		usage += " [command]"
	}

	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintf(out, "  %s\n\n", usage)
}

func writeAvailableCommands(out io.Writer, entries []helpEntry) {
	maxWidth := 0
	for _, entry := range entries {
		if len(entry.Path) > maxWidth {
			maxWidth = len(entry.Path)
		}
	}

	_, _ = fmt.Fprintln(out, "Available Commands:")
	for _, entry := range entries {
		padding := strings.Repeat(" ", max(2, maxWidth-len(entry.Path)+2))
		_, _ = fmt.Fprintf(out, "  %s%s%s\n", entry.Path, padding, entry.Short)
	}
	_, _ = fmt.Fprintln(out)
}

func writeFlagSections(out io.Writer, cmd *cobra.Command) {
	localUsages := cmd.LocalFlags().FlagUsagesWrapped(80)
	if strings.TrimSpace(localUsages) != "" {
		_, _ = fmt.Fprintln(out, "Flags:")
		_, _ = fmt.Fprint(out, localUsages)
		_, _ = fmt.Fprintln(out)
	}

	inheritedUsages := cmd.InheritedFlags().FlagUsagesWrapped(80)
	if strings.TrimSpace(inheritedUsages) != "" {
		_, _ = fmt.Fprintln(out, "Global Flags:")
		_, _ = fmt.Fprint(out, inheritedUsages)
	}
}

func flattenedHelpEntries(cmd *cobra.Command) []helpEntry {
	var direct []helpEntry
	var nested []helpEntry

	for _, child := range visibleChildren(cmd) {
		if isLeafCommand(child) {
			direct = append(direct, helpEntry{
				Path:  child.Name(),
				Short: child.Short,
			})
			continue
		}

		nested = append(nested, collectLeafDescendants(child, child.Name())...)
	}

	sort.Slice(direct, func(i, j int) bool {
		return direct[i].Path < direct[j].Path
	})
	sort.Slice(nested, func(i, j int) bool {
		return nested[i].Path < nested[j].Path
	})

	return append(direct, nested...)
}

func collectLeafDescendants(cmd *cobra.Command, prefix string) []helpEntry {
	var entries []helpEntry

	for _, child := range visibleChildren(cmd) {
		path := prefix + " " + child.Name()
		if isLeafCommand(child) {
			entries = append(entries, helpEntry{
				Path:  path,
				Short: child.Short,
			})
			continue
		}
		entries = append(entries, collectLeafDescendants(child, path)...)
	}

	return entries
}

func visibleChildren(cmd *cobra.Command) []*cobra.Command {
	children := make([]*cobra.Command, 0, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		children = append(children, child)
	}
	return children
}

func isLeafCommand(cmd *cobra.Command) bool {
	return len(visibleChildren(cmd)) == 0
}
