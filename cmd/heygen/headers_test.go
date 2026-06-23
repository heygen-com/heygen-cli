package main

import (
	"testing"
)

func TestParseAndValidateHeaders_Valid(t *testing.T) {
	hdrs, err := parseAndValidateHeaders([]string{"X-HeyGen-Client-Source: media-use"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdrs["X-Heygen-Client-Source"] != "media-use" {
		t.Errorf("got %q, want %q", hdrs["X-Heygen-Client-Source"], "media-use")
	}
}

func TestParseAndValidateHeaders_CaseNormalization(t *testing.T) {
	hdrs, err := parseAndValidateHeaders([]string{"x-heygen-client-source: my-tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdrs["X-Heygen-Client-Source"] != "my-tool" {
		t.Errorf("got %q, want %q", hdrs["X-Heygen-Client-Source"], "my-tool")
	}
}

func TestParseAndValidateHeaders_NotAllowlisted(t *testing.T) {
	_, err := parseAndValidateHeaders([]string{"Authorization: Bearer token"})
	if err == nil {
		t.Fatal("expected error for non-allowlisted header")
	}
}

func TestParseAndValidateHeaders_MalformedNoColon(t *testing.T) {
	_, err := parseAndValidateHeaders([]string{"garbage"})
	if err == nil {
		t.Fatal("expected error for malformed header")
	}
}

func TestParseAndValidateHeaders_InvalidValueChars(t *testing.T) {
	_, err := parseAndValidateHeaders([]string{"X-HeyGen-Client-Source: bad value!!"})
	if err == nil {
		t.Fatal("expected error for invalid value chars")
	}
}

func TestParseAndValidateHeaders_DuplicateCaseCollapse(t *testing.T) {
	hdrs, err := parseAndValidateHeaders([]string{
		"X-HeyGen-Client-Source: first",
		"x-heygen-client-source: second",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hdrs["X-Heygen-Client-Source"] != "second" {
		t.Errorf("duplicate collapse: got %q, want last-wins %q", hdrs["X-Heygen-Client-Source"], "second")
	}
}
