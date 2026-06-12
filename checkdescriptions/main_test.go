package main

import (
	"reflect"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
)

func TestScan(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"empty", "", nil},
		{"clean cli text", "Run 'heygen voice list' to find voices.", nil},
		{"http verb", "Pass to POST /v3/video-agents.", []string{"http-verb", "version-path"}},
		{"version path only", "See /v2/video_translate for details.", []string{"version-path"}},
		{"poll word", "Use this to poll the clone status.", []string{"poll"}},
		{"polling word", "Video ID for polling.", []string{"poll"}},
		// Deliberately NOT markers: these are accurate CLI-neutral text.
		{"cursor not flagged", "Opaque cursor token from next_token.", nil},
		{"presigned not flagged", "Presigned URL to download the file.", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scan(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scan(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestSchemaFieldDescriptions_Nested(t *testing.T) {
	spec := &command.Spec{
		Endpoint:       "/v3/example",
		Method:         "GET",
		ResponseSchema: `{"properties":{"data":{"properties":{"foo":{"description":"poll via GET /v3/x"}}}}}`,
	}
	got := schemaFieldDescriptions(spec)
	if got["response field foo"] != "poll via GET /v3/x" {
		t.Fatalf("nested field not collected: %#v", got)
	}
}
