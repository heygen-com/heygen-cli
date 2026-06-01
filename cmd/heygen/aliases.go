package main

import (
	"fmt"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/spf13/cobra"
)

// Alias re-registers a previously shipped command path that a spec-driven
// rename has since moved, so existing scripts keep working.
//
// The command tree is derived from the OpenAPI spec, and that spec is owned
// upstream (experiment-framework). When a tag or path changes upstream, the
// generated command name changes with it. An Alias points the old invocation
// at the canonical command's Spec, registering it hidden and deprecated: the
// old path still resolves to the exact same handler, while a deprecation
// notice nudges callers to the new name.
//
// Aliases are hand-maintained here, not in gen/, because they encode naming
// history that the current spec has no knowledge of. They survive
// regeneration for the same reason curated --human columns do.
type Alias struct {
	// OldParentPath is the pre-rename parent command path from the root,
	// excluding the leaf verb. For the old "heygen brand-kit list" this is
	// {"brand-kit"}; the leaf ("list") comes from Spec.
	OldParentPath []string
	// Spec is the canonical command this alias delegates to. The leaf verb,
	// flags, and args all come from the Spec, so the alias can never drift
	// from the real command.
	Spec *command.Spec
	// NewPath is the canonical invocation shown in the deprecation notice,
	// without the "heygen " prefix. Example: "brand kits list".
	NewPath string
	// RemoveBy is the release in which this alias should be deleted. Purely
	// informational; surfaced in the deprecation notice.
	RemoveBy string
}

// DeprecatedAliases is the registry of backward-compatible command aliases.
// Add an entry when a spec-driven rename changes a command path that shipped
// in a stable release.
var DeprecatedAliases = []Alias{
	{
		// "heygen brand-kit list" shipped through v0.0.11. EF 6ced9812 retagged
		// /v3/brand-kits from "Brand Kit" to "Brand", consolidating it with brand
		// glossaries under the "brand" group as "heygen brand kits list".
		OldParentPath: []string{"brand-kit"},
		Spec:          gen.BrandKitsList,
		NewPath:       "brand kits list",
		RemoveBy:      "v0.1.0",
	},
}

func (a Alias) deprecationNotice() string {
	msg := fmt.Sprintf("use %q instead", "heygen "+a.NewPath)
	if a.RemoveBy != "" {
		msg += fmt.Sprintf("; this alias will be removed in %s", a.RemoveBy)
	}
	return msg
}

// attachDeprecatedAliases registers the package-level DeprecatedAliases onto
// the command tree. Call it after the generated groups are registered so the
// canonical commands the aliases delegate to already exist.
func attachDeprecatedAliases(root *cobra.Command, ctx *cmdContext) {
	attachAliases(root, ctx, DeprecatedAliases)
}

// attachAliases registers each alias as a hidden, deprecated command that
// delegates to its canonical Spec. Split from attachDeprecatedAliases so tests
// can pass a synthetic alias set without mutating the package global.
func attachAliases(root *cobra.Command, ctx *cmdContext, aliases []Alias) {
	for _, alias := range aliases {
		if alias.Spec == nil {
			continue
		}
		parent := root
		for _, token := range alias.OldParentPath {
			parent = ensureHiddenGroup(parent, token)
		}
		leaf := buildCobraCommand(alias.Spec, ctx)
		leaf.Hidden = true
		leaf.Deprecated = alias.deprecationNotice()
		parent.AddCommand(leaf)
	}
}

// ensureHiddenGroup returns the child group named token under parent, creating
// it hidden if absent. Unlike ensureIntermediateCommand it marks newly created
// groups hidden, since alias parents must not appear in help.
func ensureHiddenGroup(parent *cobra.Command, token string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == token {
			return child
		}
	}
	child := newCommandGroup(token, humanizeCommandToken(token)+" commands")
	child.Hidden = true
	parent.AddCommand(child)
	return child
}
