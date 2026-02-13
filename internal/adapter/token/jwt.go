package token

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tartine-studio/harmony-server/internal/domain"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID string           `json:"userId"`
	Type   domain.TokenType `json:"type"`
}

type JWTService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewJwtService(secret string, accessTTL, refreshTTL time.Duration) *JWTService {
	return &JWTService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *JWTService) GenerateTokenPair(userID string) (domain.TokenPair, error) {
	access, err := s.generateToken(userID, domain.AccessToken, s.accessTTL)
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf("generate access token: %w", err)
	}

	refresh, err := s.generateToken(userID, domain.RefreshToken, s.refreshTTL)
	if err != nil {
		return domain.TokenPair{}, fmt.Errorf("generate refresh token: %w", err)
	}

	return domain.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *JWTService) ValidateToken(tokenString string) (*domain.AuthClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return &domain.AuthClaims{
		UserID: claims.UserID,
		Type:   claims.Type,
	}, nil
}

func (s *JWTService) generateToken(userID string, tokenType domain.TokenType, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		UserID: userID,
		Type:   tokenType,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}
