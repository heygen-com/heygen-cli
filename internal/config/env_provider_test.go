package config

import "testing"

func TestEnvProvider_BaseURL(t *testing.T) {
	p := &EnvProvider{}

	t.Run("default", func(t *testing.T) {
		t.Setenv(envBaseURL, "")
		if got := p.BaseURL(); got != DefaultBaseURL {
			t.Fatalf("BaseURL = %q, want %q", got, DefaultBaseURL)
		}
	})

	t.Run("custom", func(t *testing.T) {
		t.Setenv(envBaseURL, "https://api-dev.heygen.com")
		if got := p.BaseURL(); got != "https://api-dev.heygen.com" {
			t.Fatalf("BaseURL = %q, want %q", got, "https://api-dev.heygen.com")
		}
	})
}

func TestEnvProvider_Output(t *testing.T) {
	p := &EnvProvider{}

	t.Run("default", func(t *testing.T) {
		t.Setenv(envOutput, "")
		if got := p.Output(); got != DefaultOutput {
			t.Fatalf("Output = %q, want %q", got, DefaultOutput)
		}
	})

	t.Run("custom", func(t *testing.T) {
		t.Setenv(envOutput, "human")
		if got := p.Output(); got != "human" {
			t.Fatalf("Output = %q, want %q", got, "human")
		}
	})
}

func TestEnvProvider_Analytics(t *testing.T) {
	p := &EnvProvider{}

	t.Run("default", func(t *testing.T) {
		t.Setenv(envNoAnalytics, "")
		if got := p.Analytics(); !got {
			t.Fatal("Analytics = false, want true")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Setenv(envNoAnalytics, "1")
		if got := p.Analytics(); got {
			t.Fatal("Analytics = true, want false")
		}
	})
}

func TestEnvProvider_AutoUpdate(t *testing.T) {
	p := &EnvProvider{}

	t.Run("default", func(t *testing.T) {
		t.Setenv(envNoAutoUpdate, "")
		if got := p.AutoUpdate(); !got {
			t.Fatal("AutoUpdate = false, want true")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Setenv(envNoAutoUpdate, "1")
		if got := p.AutoUpdate(); got {
			t.Fatal("AutoUpdate = true, want false")
		}
	})
}

func TestEnvProvider_GetEnv(t *testing.T) {
	p := &EnvProvider{}
	t.Setenv(envOutput, "human")
	t.Setenv(envNoAnalytics, "1")

	if val, ok := p.GetEnv(KeyOutput); !ok || val != "human" {
		t.Fatalf("GetEnv(output) = (%q, %v), want (%q, true)", val, ok, "human")
	}
	if val, ok := p.GetEnv(KeyAnalytics); !ok || val != "1" {
		t.Fatalf("GetEnv(analytics) = (%q, %v), want (%q, true)", val, ok, "1")
	}
	if _, ok := p.GetEnv("bogus"); ok {
		t.Fatal("expected bogus key lookup to report unset")
	}
}
