package domain

type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

type AuthClaims struct {
	UserID string
	Type   TokenType
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

type TokenProvider interface {
	GenerateTokenPair(userId string) (TokenPair, error)
	ValidateToken(token string) (*AuthClaims, error)
}
