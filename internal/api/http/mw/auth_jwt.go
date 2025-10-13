package mw

import (
	"context"
	"dexcelerate/internal/security"
	"errors"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

// Key for claims in ctx
type claimsCtxKey struct{}

type JWTMiddleware struct {
	verifier *security.RS256Verifier // may be nil if JWT.enabled=false
}

func NewJWTMiddleware(v *security.RS256Verifier) (*JWTMiddleware, error) {
	if v == nil {
		return nil, errors.New("JWT verifier cannot be nil")
	}
	return &JWTMiddleware{verifier: v}, nil
}

func (m *JWTMiddleware) Handler(next http.Handler) http.Handler {
	if m.verifier == nil { // jwt.enabled=false -> allowed
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claimsAny, err := m.verifier.VerifyBearer(r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		rc, ok := claimsAny.(*jwt.RegisteredClaims)
		if !ok {
			http.Error(w, "invalid token claims", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsCtxKey{}, rc.Subject)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
