package auth

import (
	"errors"
	"path/filepath"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/heygen-com/heygen-cli/internal/paths"
)

// ChainCredentialResolver tries multiple credential sources in priority order.
type ChainCredentialResolver struct {
	Resolvers []CredentialResolver
}

// Resolve returns the first successful credential. Absent sources are skipped;
// broken sources surface immediately.
func (c *ChainCredentialResolver) Resolve() (string, error) {
	result, err := c.ResolveWithSource()
	if err != nil {
		return "", err
	}
	return result.Key, nil
}

// ResolveWithSource returns the first successful credential along with which
// resolver provided it. The Source tag lets callers produce source-aware error
// messages (e.g. "your env var is invalid" vs "your stored key is invalid").
func (c *ChainCredentialResolver) ResolveWithSource() (CredentialResult, error) {
	cred, err := c.ResolveTypedCredential()
	if err != nil {
		return CredentialResult{}, err
	}
	// Legacy callers only understand the API-key form. OAuth credentials
	// surface as a typed error so callers don't silently feed an
	// access_token into the x-api-key header.
	if cred.Type != CredentialTypeAPIKey {
		credPath := filepath.Join(paths.ConfigDir(), "credentials")
		return CredentialResult{}, clierrors.NewAuth(
			"stored credential is an OAuth session, not an API key",
			"This call site has not yet been wired for OAuth (file: "+credPath+")",
		)
	}
	return CredentialResult{Key: cred.APIKey, Source: cred.Source}, nil
}

// ResolveTypedCredential returns the first successful typed credential
// from the chain. Resolvers that implement TypedCredentialResolver are
// queried for the rich form; resolvers that only implement Resolve()
// have their string result wrapped as a CredentialTypeAPIKey credential.
func (c *ChainCredentialResolver) ResolveTypedCredential() (*Credential, error) {
	for _, r := range c.Resolvers {
		cred, err := resolverCredential(r)
		if err == nil {
			if cred.Source == "" {
				cred.Source = sourceForResolver(r)
			}
			return cred, nil
		}

		var notConfigured *ErrNotConfigured
		if errors.As(err, &notConfigured) {
			continue
		}

		credPath := filepath.Join(paths.ConfigDir(), "credentials")
		return nil, clierrors.NewAuth(err.Error(), "Check the credentials file at "+credPath)
	}

	return nil, clierrors.NewAuth(
		"no API key found",
		"Set HEYGEN_API_KEY env var or run: heygen auth login",
	)
}

// resolverCredential dispatches to the typed resolver method when the
// resolver supports it, otherwise wraps the string form.
func resolverCredential(r CredentialResolver) (*Credential, error) {
	if typed, ok := r.(TypedCredentialResolver); ok {
		return typed.ResolveCredential()
	}
	key, err := r.Resolve()
	if err != nil {
		return nil, err
	}
	return &Credential{
		Type:   CredentialTypeAPIKey,
		APIKey: key,
		Source: sourceForResolver(r),
	}, nil
}

func sourceForResolver(r CredentialResolver) CredentialSource {
	switch r.(type) {
	case *EnvCredentialResolver:
		return SourceEnv
	case *FileCredentialResolver:
		return SourceFile
	default:
		return ""
	}
}
