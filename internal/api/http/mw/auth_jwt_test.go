package mw

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"dexcelerate/internal/security"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generate test RSA keys
func generateTestKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return privateKey, &privateKey.PublicKey
}

// create test JWT token
func createTestToken(t *testing.T, privateKey *rsa.PrivateKey, sub, aud, iss string, expiry time.Duration) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   sub,
		Audience:  jwt.ClaimStrings{aud},
		Issuer:    iss,
		ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		IssuedAt:  jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return tokenString
}

func TestNewJWTMiddleware(t *testing.T) {
	t.Run("panic_when_verifier_is_nil", func(t *testing.T) {
		_, err := NewJWTMiddleware(nil)
		assert.Error(t, err)
	})

	t.Run("successful_creation", func(t *testing.T) {
		_, pubKey := generateTestKeys(t)
		verifier := &security.RS256Verifier{
			PubKey: pubKey,
			Aud:    "test-aud",
			Iss:    "test-iss",
		}

		middleware, err := NewJWTMiddleware(verifier)
		require.NoError(t, err)
		assert.NotNil(t, middleware)
		assert.Equal(t, verifier, middleware.verifier)
	})
}

func TestJWTMiddleware_Handler_Success(t *testing.T) {
	privKey, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "test-issuer",
		Leeway: 10 * time.Second,
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	token := createTestToken(t, privKey, "user123", "test-service", "test-issuer", 1*time.Hour)

	nextHandlerCalled := false
	var capturedSubject string

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		if v := r.Context().Value(claimsCtxKey{}); v != nil {
			if s, ok := v.(string); ok {
				capturedSubject = s
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, nextHandlerCalled, "next handler should be called")
	assert.Equal(t, "user123", capturedSubject, "subject should be extracted to context")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "success", rec.Body.String())
}

func TestJWTMiddleware_Handler_MissingToken(t *testing.T) {
	_, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "test-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	testCases := []struct {
		name          string
		authHeader    string
		expectedError string
	}{
		{
			name:          "no_authorization_header",
			authHeader:    "",
			expectedError: "authorization header must be: Bearer <token>",
		},
		{
			name:          "empty_authorization_header",
			authHeader:    "   ",
			expectedError: "authorization header must be: Bearer <token>",
		},
		{
			name:          "missing_bearer_prefix",
			authHeader:    "sometoken",
			expectedError: "authorization header must be: Bearer <token>",
		},
		{
			name:          "only_bearer_word",
			authHeader:    "Bearer",
			expectedError: "authorization header must be: Bearer <token>",
		},
		{
			name:          "bearer_with_empty_token",
			authHeader:    "Bearer   ",
			expectedError: "authorization header must be: Bearer <token>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nextHandlerCalled = false

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.False(t, nextHandlerCalled, "next handler should not be called")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.expectedError)
		})
	}
}

func TestJWTMiddleware_Handler_InvalidToken(t *testing.T) {
	_, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "test-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	testCases := []struct {
		name  string
		token string
	}{
		{
			name:  "malformed_token",
			token: "not.a.valid.jwt.token",
		},
		{
			name:  "random_string",
			token: "randomstringnottoken",
		},
		{
			name:  "base64_but_not_jwt",
			token: "dGVzdC10b2tlbg==",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nextHandlerCalled = false

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.False(t, nextHandlerCalled, "next handler should not be called")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestJWTMiddleware_Handler_ExpiredToken(t *testing.T) {
	privKey, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "test-issuer",
		Leeway: 0, // No leeway for this test
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	// create expired token
	token := createTestToken(t, privKey, "user123", "test-service", "test-issuer", -1*time.Hour)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, nextHandlerCalled, "next handler should not be called for expired token")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWTMiddleware_Handler_WrongAudience(t *testing.T) {
	privKey, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "expected-audience",
		Iss:    "test-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	// create token with not valid audience
	token := createTestToken(t, privKey, "user123", "wrong-audience", "test-issuer", 1*time.Hour)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, nextHandlerCalled, "next handler should not be called for wrong audience")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWTMiddleware_Handler_WrongIssuer(t *testing.T) {
	privKey, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "expected-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	// create token with not valid issuer
	token := createTestToken(t, privKey, "user123", "test-service", "wrong-issuer", 1*time.Hour)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, nextHandlerCalled, "next handler should not be called for wrong issuer")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWTMiddleware_Handler_WrongSignature(t *testing.T) {
	privKey1, _ := generateTestKeys(t)
	_, pubKey2 := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey2,
		Aud:    "test-service",
		Iss:    "test-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	token := createTestToken(t, privKey1, "user123", "test-service", "test-issuer", 1*time.Hour)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.False(t, nextHandlerCalled, "next handler should not be called for wrong signature")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestJWTMiddleware_Handler_ContextPropagation(t *testing.T) {
	privKey, pubKey := generateTestKeys(t)

	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-service",
		Iss:    "test-issuer",
	}

	middleware, err := NewJWTMiddleware(verifier)
	require.NoError(t, err)

	token := createTestToken(t, privKey, "user-with-special-id", "test-service", "test-issuer", 1*time.Hour)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := subjectFromContext(r)
		assert.Equal(t, "user-with-special-id", sub)

		if originalValue := r.Context().Value("test-key"); originalValue != nil {
			assert.Equal(t, "test-value", originalValue.(string))
		}

		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// add value to origin ctx
	ctx := context.WithValue(req.Context(), "test-key", "test-value")
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestJWTMiddleware_Handler_NilVerifier(t *testing.T) {
	// this test check when verifier == nil
	middleware := &JWTMiddleware{verifier: nil}

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// when verifier nil, mw must pass next request
	assert.True(t, nextHandlerCalled, "next handler should be called when verifier is nil")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSubjectFromContext(t *testing.T) {
	t.Run("subject_exists", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), claimsCtxKey{}, "test-subject")
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(ctx)

		subject := subjectFromContext(req)
		assert.Equal(t, "test-subject", subject)
	})

	t.Run("subject_not_exists", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		subject := subjectFromContext(req)
		assert.Equal(t, "", subject)
	})

	t.Run("wrong_type_in_context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), claimsCtxKey{}, 12345) // int instead of string
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(ctx)

		subject := subjectFromContext(req)
		assert.Equal(t, "", subject)
	})
}
