package main

import (
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/command"
)

func TestTimeoutForSpec(t *testing.T) {
	tests := []struct {
		name string
		spec *command.Spec
		want time.Duration
	}{
		{"multipart upload -> upload budget", &command.Spec{Group: "asset", Name: "create", BodyEncoding: "multipart"}, timeoutUpload},
		{"pollable create -> create budget", &command.Spec{Group: "video", Name: "create", BodyEncoding: "json"}, timeoutCreate},
		{"plain read -> default budget", &command.Spec{Group: "video", Name: "list", Method: "GET"}, timeoutRead},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeoutForSpec(tt.spec); got != tt.want {
				t.Errorf("timeoutForSpec = %v, want %v", got, tt.want)
			}
		})
	}
}
