package main

import (
	"io"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/heygen-com/heygen-cli/internal/output"
	"github.com/spf13/cobra"
)

func testCmdContext() *cmdContext {
	return &cmdContext{
		formatter: output.NewJSONFormatter(io.Discard, io.Discard),
		version:   "test",
	}
}

func findChild(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestAliasDeprecationNotice(t *testing.T) {
	cases := []struct {
		name  string
		alias Alias
		want  string
	}{
		{
			name:  "with remove-by",
			alias: Alias{NewPath: "brand kits list", RemoveBy: "v0.2.0"},
			want:  `use "heygen brand kits list" instead; this alias will be removed in v0.2.0`,
		},
		{
			name:  "without remove-by",
			alias: Alias{NewPath: "brand kits list"},
			want:  `use "heygen brand kits list" instead`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.alias.deprecationNotice(); got != tc.want {
				t.Errorf("deprecationNotice() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAttachAliasesRegistersHiddenDeprecatedLeaf(t *testing.T) {
	root := &cobra.Command{Use: "heygen"}
	spec := &command.Spec{
		Group:    "brand",
		Name:     "kits list",
		Method:   "GET",
		Endpoint: "/v3/brand-kits",
		Summary:  "List Brand Kits",
	}
	aliases := []Alias{{
		OldParentPath: []string{"brand-kit"},
		Spec:          spec,
		NewPath:       "brand kits list",
		RemoveBy:      "v0.2.0",
	}}

	attachAliases(root, testCmdContext(), aliases)

	parent := findChild(root, "brand-kit")
	if parent == nil {
		t.Fatal(`expected hidden parent group "brand-kit" to be registered`)
	}
	if !parent.Hidden {
		t.Error("alias parent group should be hidden from help")
	}

	leaf := findChild(parent, "list")
	if leaf == nil {
		t.Fatal(`expected alias leaf "list" under "brand-kit"`)
	}
	if !leaf.Hidden {
		t.Error("alias leaf should be hidden from help")
	}
	if leaf.Deprecated == "" {
		t.Error("alias leaf should carry a deprecation notice")
	}
	if leaf.RunE == nil {
		t.Error("alias leaf should delegate to the canonical command's RunE")
	}
}

func TestAttachAliasesSkipsNilSpec(t *testing.T) {
	root := &cobra.Command{Use: "heygen"}
	attachAliases(root, testCmdContext(), []Alias{{OldParentPath: []string{"ghost"}, Spec: nil}})
	if findChild(root, "ghost") != nil {
		t.Error("alias with a nil Spec should be skipped, not registered")
	}
}

func TestAttachAliasesSharesParentGroup(t *testing.T) {
	root := &cobra.Command{Use: "heygen"}
	specList := &command.Spec{Group: "brand", Name: "kits list", Method: "GET", Endpoint: "/v3/brand-kits"}
	specGet := &command.Spec{
		Group:    "brand",
		Name:     "kits get",
		Method:   "GET",
		Endpoint: "/v3/brand-kits/{id}",
		Args:     []command.ArgSpec{{Name: "id", Param: "id"}},
	}
	attachAliases(root, testCmdContext(), []Alias{
		{OldParentPath: []string{"brand-kit"}, Spec: specList, NewPath: "brand kits list"},
		{OldParentPath: []string{"brand-kit"}, Spec: specGet, NewPath: "brand kits get"},
	})

	parents := 0
	for _, c := range root.Commands() {
		if c.Name() == "brand-kit" {
			parents++
		}
	}
	if parents != 1 {
		t.Fatalf(`expected a single shared "brand-kit" parent group, got %d`, parents)
	}
}
