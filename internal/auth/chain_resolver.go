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
	for _, r := range c.Resolvers {
		key, err := r.Resolve()
		if err == nil {
			return key, nil
		}

		var notConfigured *ErrNotConfigured
		if errors.As(err, &notConfigured) {
			continue
		}

		credPath := filepath.Join(paths.ConfigDir(), "credentials")
		return "", clierrors.NewAuth(err.Error(), "Check the credentials file at "+credPath)
	}

	return "", clierrors.NewAuth(
		"no API key found",
		"Set HEYGEN_API_KEY env var or run: heygen auth login",
	)
}
