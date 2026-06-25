package oauth

import (
	"testing"
	"time"
)

func TestTokenResponse_Expired(t *testing.T) {
	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		tok         *TokenResponse
		now         time.Time
		skew        time.Duration
		wantExpired bool
	}{
		{
			name:        "nil token is expired",
			tok:         nil,
			now:         base,
			wantExpired: true,
		},
		{
			name:        "zero IssuedAt: no information, treat as live",
			tok:         &TokenResponse{ExpiresIn: 3600},
			now:         base,
			wantExpired: false,
		},
		{
			name:        "zero ExpiresIn: no information, treat as live",
			tok:         &TokenResponse{IssuedAt: base},
			now:         base,
			wantExpired: false,
		},
		{
			name:        "well within lifetime",
			tok:         &TokenResponse{IssuedAt: base, ExpiresIn: 3600},
			now:         base.Add(30 * time.Minute),
			wantExpired: false,
		},
		{
			name:        "exactly at expiry: expired",
			tok:         &TokenResponse{IssuedAt: base, ExpiresIn: 3600},
			now:         base.Add(time.Hour),
			wantExpired: true,
		},
		{
			name:        "past expiry: expired",
			tok:         &TokenResponse{IssuedAt: base, ExpiresIn: 3600},
			now:         base.Add(2 * time.Hour),
			wantExpired: true,
		},
		{
			name:        "skew makes a live token look expired (proactive refresh)",
			tok:         &TokenResponse{IssuedAt: base, ExpiresIn: 3600},
			now:         base.Add(59 * time.Minute),
			skew:        2 * time.Minute,
			wantExpired: true,
		},
		{
			name:        "skew leaves headroom",
			tok:         &TokenResponse{IssuedAt: base, ExpiresIn: 3600},
			now:         base.Add(57 * time.Minute),
			skew:        2 * time.Minute,
			wantExpired: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.tok.Expired(tc.now, tc.skew)
			if got != tc.wantExpired {
				t.Errorf("Expired() = %v, want %v", got, tc.wantExpired)
			}
		})
	}
}
