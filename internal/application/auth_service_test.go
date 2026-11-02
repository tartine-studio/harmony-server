package application

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tartine-studio/harmony-server/internal/adapter/token"
	"github.com/tartine-studio/harmony-server/internal/domain"
)

func TestMain(m *testing.M) {
	BcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

func newAuthService() (*AuthService, *fakeUserRepo, *token.JWTService) {
	repo := newFakeUserRepo()
	jwt := token.NewJwtService("test-secret", time.Minute, time.Hour)
	return NewAuthService(repo, jwt), repo, jwt
}

func TestAuthService_Register(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newAuthService()

	user, err := svc.Register(ctx, "alice", "alice@example.com", "password123")
	require.NoError(t, err)
	require.NotEmpty(t, user.ID)
	require.Equal(t, "alice", user.Username)
	require.NotEqual(t, "password123", user.Password, "password must be hashed")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("password123")))

	stored, _ := repo.GetByID(ctx, user.ID)
	require.NotNil(t, stored)

	_, err = svc.Register(ctx, "alice2", "alice@example.com", "password456")
	require.ErrorIs(t, err, ErrEmailTaken)
}

func TestAuthService_Login(t *testing.T) {
	ctx := context.Background()
	svc, _, jwt := newAuthService()
	user, err := svc.Register(ctx, "alice", "alice@example.com", "password123")
	require.NoError(t, err)

	pair, err := svc.Login(ctx, "alice@example.com", "password123")
	require.NoError(t, err)
	claims, err := jwt.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, domain.AccessToken, claims.Type)

	tests := map[string]struct{ email, password string }{
		"wrong password": {"alice@example.com", "nope-nope"},
		"unknown email":  {"bob@example.com", "password123"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Login(ctx, tc.email, tc.password)
			require.ErrorIs(t, err, ErrInvalidCredentials)
		})
	}
}

func TestAuthService_Refresh(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newAuthService()
	user, err := svc.Register(ctx, "alice", "alice@example.com", "password123")
	require.NoError(t, err)
	pair, err := svc.Login(ctx, "alice@example.com", "password123")
	require.NoError(t, err)

	t.Run("refresh token", func(t *testing.T) {
		next, err := svc.Refresh(ctx, pair.RefreshToken)
		require.NoError(t, err)
		require.NotEmpty(t, next.AccessToken)
	})

	t.Run("access token is rejected", func(t *testing.T) {
		_, err := svc.Refresh(ctx, pair.AccessToken)
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("garbage", func(t *testing.T) {
		_, err := svc.Refresh(ctx, "not-a-jwt")
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("deleted user", func(t *testing.T) {
		require.NoError(t, repo.Delete(ctx, user.ID))
		_, err := svc.Refresh(ctx, pair.RefreshToken)
		require.ErrorIs(t, err, ErrInvalidToken)
	})
}
