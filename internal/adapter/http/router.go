package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"

	authmw "github.com/tartine-studio/harmony-server/internal/adapter/http/middleware"
	"github.com/tartine-studio/harmony-server/internal/domain"
)

type Dependencies struct {
	AuthHandler    *AuthHandler
	UserHandler    *UserHandler
	ChannelHandler *ChannelHandler
	JWTService     domain.TokenProvider
	Logger         *zap.Logger
}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(requestLogger(deps.Logger))
	r.Use(chimw.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", deps.AuthHandler.Register)
			r.Post("/login", deps.AuthHandler.Login)
			r.Post("/refresh", deps.AuthHandler.Refresh)
		})

		r.Group(func(r chi.Router) {
			r.Use(authmw.IsAuthenticated(deps.JWTService))

			r.Route("/users", func(r chi.Router) {
				r.Get("/me", deps.UserHandler.Me)
				r.Patch("/me", deps.UserHandler.UpdateMe)
				r.Get("/", deps.UserHandler.GetAll)
				r.Get("/{id}", deps.UserHandler.GetByID)
				r.Patch("/{id}", deps.UserHandler.Update)
				r.Delete("/{id}", deps.UserHandler.Delete)
			})

			r.Route("/channels", func(r chi.Router) {
				r.Get("/", deps.ChannelHandler.GetAll)
				r.Post("/", deps.ChannelHandler.Create)
				r.Get("/{id}", deps.ChannelHandler.GetByID)
				r.Patch("/{id}", deps.ChannelHandler.Update)
				r.Delete("/{id}", deps.ChannelHandler.Delete)
			})
		})
	})

	return r
}

func requestLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.Status()),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
