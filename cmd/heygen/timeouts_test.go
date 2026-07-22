package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/command"
)

func TestTimeoutForSpec(t *testing.T) {
	tests := []struct {
		name string
		spec *command.Spec
		want time.Duration
	}{
		{"multipart upload -> upload budget", &command.Spec{Method: http.MethodPost, BodyEncoding: "multipart"}, timeoutUpload},
		{"GET read -> read budget", &command.Spec{Method: http.MethodGet}, timeoutRead},
		{"POST write -> write budget", &command.Spec{Method: http.MethodPost, BodyEncoding: "json"}, timeoutWrite},
		{"DELETE write -> write budget", &command.Spec{Method: http.MethodDelete}, timeoutWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeoutForSpec(tt.spec); got != tt.want {
				t.Errorf("timeoutForSpec = %v, want %v", got, tt.want)
			}
		})
	}
}

// Async-create endpoints that lack a hand-written poll config must NOT fall to
// the 30s read budget — that was the regression the method-based classification
// fixes (an earlier version keyed off pollConfigs, which covers only 4 of the
// async creates). Assert they get a write-or-larger budget, not read.
func TestTimeoutForSpec_AsyncCreatesNotRead(t *testing.T) {
	for _, spec := range []*command.Spec{
		gen.BackgroundRemovalCreate,
		gen.AiClippingCreate,
		gen.AvatarCreate,
		gen.VoiceCloneCreate,
	} {
		if got := timeoutForSpec(spec); got == timeoutRead {
			t.Errorf("%s %s got the 30s read budget; async creates must get a larger budget", spec.Group, spec.Name)
		}
	}
}
