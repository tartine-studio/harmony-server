package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tartine-studio/harmony-server/internal/domain"
)

func TestJWT_RoundTrip(t *testing.T) {
	svc := NewJwtService("secret", time.Minute, time.Hour)

	pair, err := svc.GenerateTokenPair("user-1")
	require.NoError(t, err)

	access, err := svc.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, &domain.AuthClaims{UserID: "user-1", Type: domain.AccessToken}, access)

	refresh, err := svc.ValidateToken(pair.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, &domain.AuthClaims{UserID: "user-1", Type: domain.RefreshToken}, refresh)
}

func TestJWT_Rejects(t *testing.T) {
	svc := NewJwtService("secret", time.Minute, time.Hour)

	expired, err := NewJwtService("secret", -time.Minute, -time.Minute).GenerateTokenPair("user-1")
	require.NoError(t, err)

	otherSecret, err := NewJwtService("other", time.Minute, time.Hour).GenerateTokenPair("user-1")
	require.NoError(t, err)

	// alg=none must never be accepted.
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{UserID: "user-1", Type: domain.AccessToken}).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := map[string]string{
		"expired":      expired.AccessToken,
		"wrong secret": otherSecret.AccessToken,
		"alg none":     none,
		"garbage":      "not.a.jwt",
		"empty":        "",
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc.ValidateToken(tok)
			require.Error(t, err)
		})
	}
}
