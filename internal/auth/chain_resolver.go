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
	for _, r := range c.Resolvers {
		key, err := r.Resolve()
		if err == nil {
			return CredentialResult{Key: key, Source: sourceForResolver(r)}, nil
		}

		var notConfigured *ErrNotConfigured
		if errors.As(err, &notConfigured) {
			continue
		}

		credPath := filepath.Join(paths.ConfigDir(), "credentials")
		return CredentialResult{}, clierrors.NewAuth(err.Error(), "Check the credentials file at "+credPath)
	}

	return CredentialResult{}, clierrors.NewAuth(
		"no API key found",
		"Set HEYGEN_API_KEY env var or run: heygen auth login",
	)
}

func sourceForResolver(r CredentialResolver) CredentialSource {
	switch r.(type) {
	case *EnvCredentialResolver:
		return SourceEnv
	case *FileCredentialResolver:
		return SourceFile
	default:
		return SourceUnknown
	}
}
