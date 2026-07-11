package main

import (
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/spf13/cobra"
)

func TestIsHiddenCommand(t *testing.T) {
	hidden := &command.Spec{Group: "asset", Name: "search", Method: "GET", Endpoint: "/v3/assets/search"}
	if !hidden.IsHidden() {
		t.Error("GET /v3/assets/search should be hidden")
	}
	// Method match is case-insensitive (specs may carry a lowercased method).
	lower := &command.Spec{Group: "asset", Name: "search", Method: "get", Endpoint: "/v3/assets/search"}
	if !lower.IsHidden() {
		t.Error("method match should be case-insensitive")
	}
	visible := &command.Spec{Group: "video", Name: "list", Method: "GET", Endpoint: "/v3/videos"}
	if visible.IsHidden() {
		t.Error("GET /v3/videos should not be hidden")
	}
}

func TestBuildCobraCommand_HiddenWiring(t *testing.T) {
	ctx := testCmdContext()

	hidden := buildCobraCommand(&command.Spec{
		Group: "asset", Name: "search", Method: "GET", Endpoint: "/v3/assets/search",
		Summary: "Search assets", Examples: []string{"heygen asset search --query x"},
	}, ctx)
	if !hidden.Hidden {
		t.Error("asset search should be Cobra-Hidden (functional but omitted from help listings)")
	}
	// A hidden command must still be runnable and have its own help.
	if hidden.RunE == nil {
		t.Error("hidden command should still be runnable (RunE wired)")
	}

	visible := buildCobraCommand(&command.Spec{
		Group: "video", Name: "list", Method: "GET", Endpoint: "/v3/videos",
		Summary: "List videos", Examples: []string{"heygen video list"},
	}, ctx)
	if visible.Hidden {
		t.Error("video list should not be hidden")
	}
}

// TestHideEmptyGroups covers the multi-word case: when a hidden leaf's only sibling-set under an
// intermediate group is itself, the intermediate group must also be hidden so it doesn't leak into
// help as a phantom entry. The leaf stays runnable.
func TestHideEmptyGroups(t *testing.T) {
	asset := &cobra.Command{Use: "asset"}
	create := &cobra.Command{Use: "create", RunE: func(*cobra.Command, []string) error { return nil }}
	search := &cobra.Command{Use: "search"}
	list := &cobra.Command{Use: "list", Hidden: true, RunE: func(*cobra.Command, []string) error { return nil }}
	search.AddCommand(list)
	asset.AddCommand(create, search)

	hideEmptyGroups(asset)

	if !search.Hidden {
		t.Error("intermediate 'search' group with only a hidden leaf should be hidden")
	}
	if asset.Hidden {
		t.Error("'asset' group with a visible child should stay visible")
	}
	if !list.Hidden || list.RunE == nil {
		t.Error("hidden leaf should remain hidden and runnable")
	}
}
