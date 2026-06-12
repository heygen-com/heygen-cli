package clidesc

import (
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/command"
)

// TestResolvers covers the override-wins and fall-through paths of the overlay
// resolvers directly.
func TestResolvers(t *testing.T) {
	// Overridden endpoint: description wins, summary falls through (no Summary
	// override defined for this entry).
	clone := &command.Spec{
		Endpoint:    "/v3/voices/clone",
		Method:      "POST",
		Summary:     "Create a voice clone",
		Description: "generated description with GET /v3/voices/{voice_clone_id}",
	}
	if got := Summary(clone); got != "Create a voice clone" {
		t.Errorf("summary should fall through, got %q", got)
	}
	if got := Description(clone); !strings.Contains(got, "heygen voice get") {
		t.Errorf("description should be overridden, got %q", got)
	}

	// Non-overridden endpoint: everything falls through.
	other := &command.Spec{
		Endpoint:    "/v3/videos",
		Method:      "GET",
		Summary:     "List videos",
		Description: "generated list description",
	}
	if got := Summary(other); got != "List videos" {
		t.Errorf("summary fall-through failed, got %q", got)
	}
	if got := Description(other); got != "generated list description" {
		t.Errorf("description fall-through failed, got %q", got)
	}
	flag := command.FlagSpec{Name: "limit", Help: "generated limit help"}
	if got := FlagHelp(other, flag); got != "generated limit help" {
		t.Errorf("flag help fall-through failed, got %q", got)
	}

	// Flag override-wins on an overridden endpoint.
	vt := &command.Spec{Endpoint: "/v3/video-translations", Method: "POST"}
	ol := command.FlagSpec{Name: "output-languages", Help: "generated GET ... help"}
	if got := FlagHelp(vt, ol); !strings.Contains(got, "heygen video-translate languages list") {
		t.Errorf("flag help override-wins failed, got %q", got)
	}
	// A flag with no override on an overridden endpoint falls through.
	plain := command.FlagSpec{Name: "title", Help: "generated title help"}
	if got := FlagHelp(vt, plain); got != "generated title help" {
		t.Errorf("unlisted flag should fall through, got %q", got)
	}

	// Schema fall-through: no Fields override → identical string returned.
	const sc = `{"properties":{"x":{"description":"d"}}}`
	if got := Schema(other, sc); got != sc {
		t.Errorf("schema with no override should be returned unchanged, got %q", got)
	}
	// Malformed schema on an overridden endpoint → original returned.
	const bad = `{not json`
	if got := Schema(clone, bad); got != bad {
		t.Errorf("malformed schema should be returned unchanged, got %q", got)
	}
}

// TestAllEndpointsExist guards against typos and stale keys: every override
// key must match a real generated command (endpoint + method) in gen.Groups.
// If an upstream resync renames an endpoint, this test fails loudly so the
// override is updated rather than silently dead.
func TestAllEndpointsExist(t *testing.T) {
	live := make(map[Key]bool)
	for _, specs := range gen.Groups {
		for _, s := range specs {
			live[Key{s.Endpoint, s.Method}] = true
		}
	}
	for key := range Overrides {
		if !live[key] {
			t.Errorf("override key {%s %s} does not match any generated command", key.Method, key.Endpoint)
		}
	}
}
