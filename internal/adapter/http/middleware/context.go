package middleware

import "context"

type contextKey int

const userContextKey contextKey = iota

type UserContext struct {
	UserID string
}

func NewUserContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userContextKey, UserContext{UserID: userID})
}

func UserFromContext(ctx context.Context) (UserContext, bool) {
	uc, ok := ctx.Value(userContextKey).(UserContext)
	return uc, ok
}
