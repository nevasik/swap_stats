package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"dexcelerate/internal/config"
	"encoding/pem"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test keys generated once for all tests
var (
	testPrivateKey     *rsa.PrivateKey
	testPublicKey      *rsa.PublicKey
	testPublicKeyPath  string
	otherPrivateKey    *rsa.PrivateKey
	otherPublicKeyPath string
)

func TestMain(m *testing.M) {
	// setup test keys
	var err error
	testPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate test private key: %v", err))
	}
	testPublicKey = &testPrivateKey.PublicKey

	otherPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate other private key: %v", err))
	}

	// create temporary files for public keys
	testPublicKeyPath = createTempPublicKey(testPublicKey)
	otherPublicKeyPath = createTempPublicKey(&otherPrivateKey.PublicKey)

	// run tests
	code := m.Run()

	// Cleanup
	os.Remove(testPublicKeyPath)
	os.Remove(otherPublicKeyPath)

	os.Exit(code)
}

func createTempPublicKey(pubKey *rsa.PublicKey) string {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal public key: %v", err))
	}

	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	tmpFile, err := os.CreateTemp("", "test_pub_key_*.pem")
	if err != nil {
		panic(fmt.Sprintf("Failed to create temp file: %v", err))
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(pubKeyPEM); err != nil {
		panic(fmt.Sprintf("Failed to write to temp file: %v", err))
	}

	return tmpFile.Name()
}

func generateTestToken(claims jwt.Claims, key *rsa.PrivateKey) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(key)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate test token: %v", err))
	}
	return tokenString
}

func TestNewRS256Verifier(t *testing.T) {
	tests := []struct {
		name        string
		pubKeyPath  string
		audience    string
		issuer      string
		wantErr     bool
		errContains string
	}{
		{
			name:       "successful creation",
			pubKeyPath: testPublicKeyPath,
			audience:   "test-Aud",
			issuer:     "test-Iss",
			wantErr:    false,
		},
		{
			name:        "file not found",
			pubKeyPath:  "/nonexistent/file.pem",
			wantErr:     true,
			errContains: "failed to read public key",
		},
		{
			name:        "invalid pem file",
			pubKeyPath:  createInvalidPemFile(),
			wantErr:     true,
			errContains: "failed to parse public key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier, err := NewRS256Verifier(&config.JWTConfig{
				Enabled:       true,
				PublicKeyPath: tt.pubKeyPath,
				Audience:      tt.audience,
				Issuer:        tt.issuer,
				Leeway:        time.Second * 30,
			})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, verifier)
			assert.Equal(t, tt.audience, verifier.Aud)
			assert.Equal(t, tt.issuer, verifier.Iss)
			assert.NotNil(t, verifier.PubKey)
		})
	}
}

func TestVerifyBearer_Success(t *testing.T) {
	verifier, err := NewRS256Verifier(&config.JWTConfig{
		Enabled:       true,
		PublicKeyPath: testPublicKeyPath,
		Audience:      "test-Aud",
		Issuer:        "test-Iss",
		Leeway:        time.Second * 30,
	})

	require.NoError(t, err)

	claims := jwt.RegisteredClaims{
		Subject:  "user123",
		Audience: jwt.ClaimStrings{"test-Aud"},
		Issuer:   "test-Iss",
		ExpiresAt: &jwt.NumericDate{
			Time: time.Now().Add(time.Hour),
		},
		IssuedAt: &jwt.NumericDate{
			Time: time.Now().Add(-time.Minute),
		},
	}

	token := generateTestToken(claims, testPrivateKey)
	authHeader := fmt.Sprintf("Bearer %s", token)

	parsedClaims, err := verifier.VerifyBearer(authHeader)
	require.NoError(t, err)

	registeredClaims, ok := parsedClaims.(*jwt.RegisteredClaims)
	require.True(t, ok)
	assert.Equal(t, "user123", registeredClaims.Subject)
}

func TestVerifyBearer_InvalidTokens(t *testing.T) {
	verifier, err := NewRS256Verifier(&config.JWTConfig{
		Enabled:       true,
		PublicKeyPath: testPublicKeyPath,
		Audience:      "test-Aud",
		Issuer:        "test-Iss",
		Leeway:        time.Second * 30,
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		setupToken  func() string
		errContains string
	}{
		{
			name: "wrong signature",
			setupToken: func() string {
				claims := jwt.RegisteredClaims{
					Subject:  "user123",
					Audience: jwt.ClaimStrings{"test-Aud"},
					Issuer:   "test-Iss",
					ExpiresAt: &jwt.NumericDate{
						Time: time.Now().Add(time.Hour),
					},
				}
				// sign with different private key.
				return generateTestToken(claims, otherPrivateKey)
			},
			errContains: "failed to parse token",
		},
		{
			name: "expired token",
			setupToken: func() string {
				claims := jwt.RegisteredClaims{
					Subject:  "user123",
					Audience: jwt.ClaimStrings{"test-Aud"},
					Issuer:   "test-Iss",
					ExpiresAt: &jwt.NumericDate{
						Time: time.Now().Add(-time.Hour),
					},
				}
				return generateTestToken(claims, testPrivateKey)
			},
			errContains: "failed to parse token",
		},
		{
			name: "wrong audience",
			setupToken: func() string {
				claims := jwt.RegisteredClaims{
					Subject:  "user123",
					Audience: jwt.ClaimStrings{"wrong-Aud"},
					Issuer:   "test-Iss",
					ExpiresAt: &jwt.NumericDate{
						Time: time.Now().Add(time.Hour),
					},
				}
				return generateTestToken(claims, testPrivateKey)
			},
			errContains: "failed to parse token",
		},
		{
			name: "wrong issuer",
			setupToken: func() string {
				claims := jwt.RegisteredClaims{
					Subject:  "user123",
					Audience: jwt.ClaimStrings{"test-Aud"},
					Issuer:   "wrong-Iss",
					ExpiresAt: &jwt.NumericDate{
						Time: time.Now().Add(time.Hour),
					},
				}
				return generateTestToken(claims, testPrivateKey)
			},
			errContains: "failed to parse token",
		},
		{
			name: "token not yet valid",
			setupToken: func() string {
				claims := jwt.RegisteredClaims{
					Subject:  "user123",
					Audience: jwt.ClaimStrings{"test-Aud"},
					Issuer:   "test-Iss",
					ExpiresAt: &jwt.NumericDate{
						Time: time.Now().Add(time.Hour),
					},
					NotBefore: &jwt.NumericDate{
						Time: time.Now().Add(time.Hour),
					},
				}
				return generateTestToken(claims, testPrivateKey)
			},
			errContains: "failed to parse token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := tt.setupToken()
			authHeader := fmt.Sprintf("Bearer %s", token)

			claims, err := verifier.VerifyBearer(authHeader)
			assert.Error(t, err)
			assert.Nil(t, claims)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

func TestVerifyBearer_Leeway(t *testing.T) {
	verifier, err := NewRS256Verifier(&config.JWTConfig{
		Enabled:       true,
		PublicKeyPath: testPublicKeyPath,
		Audience:      "",
		Issuer:        "",
		Leeway:        time.Second * 30,
	})
	require.NoError(t, err)

	// token expired 30 seconds ago, but Leeway is 1 minute.
	claims := jwt.RegisteredClaims{
		Subject: "user123",
		ExpiresAt: &jwt.NumericDate{
			Time: time.Now().Add(-29 * time.Second),
		},
		IssuedAt: &jwt.NumericDate{
			Time: time.Now().Add(-2 * time.Minute),
		},
	}

	token := generateTestToken(claims, testPrivateKey)
	authHeader := fmt.Sprintf("Bearer %s", token)

	parsedClaims, err := verifier.VerifyBearer(authHeader)
	require.NoError(t, err)
	assert.NotNil(t, parsedClaims)
}

func TestVerifyBearer_WithoutAudienceIssuer(t *testing.T) {
	// verifier without audience/issuer checks.
	verifier, err := NewRS256Verifier(&config.JWTConfig{
		Enabled:       true,
		PublicKeyPath: testPublicKeyPath,
		Audience:      "",
		Issuer:        "",
	})
	require.NoError(t, err)

	claims := jwt.RegisteredClaims{
		Subject: "user123",
		ExpiresAt: &jwt.NumericDate{
			Time: time.Now().Add(time.Hour),
		},
	}

	token := generateTestToken(claims, testPrivateKey)
	authHeader := fmt.Sprintf("Bearer %s", token)

	parsedClaims, err := verifier.VerifyBearer(authHeader)
	require.NoError(t, err)
	assert.NotNil(t, parsedClaims)
}

func TestExtractBearer(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantToken   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid bearer token",
			header:    "Bearer valid-token",
			wantToken: "valid-token",
			wantErr:   false,
		},
		{
			name:      "valid bearer token with spaces",
			header:    "Bearer   token-with-spaces   ",
			wantToken: "token-with-spaces",
			wantErr:   false,
		},
		{
			name:        "empty header",
			header:      "",
			wantErr:     true,
			errContains: ErrNoBearerToken.Error(),
		},
		{
			name:        "missing bearer prefix",
			header:      "Token abc",
			wantErr:     true,
			errContains: ErrNoBearerToken.Error(),
		},
		{
			name:        "only bearer word",
			header:      "Bearer ",
			wantErr:     true,
			errContains: ErrNoBearerToken.Error(),
		},
		{
			name:      "case insensitive bearer",
			header:    "bearer token-lowercase",
			wantToken: "token-lowercase",
			wantErr:   false,
		},
		{
			name:      "multiple spaces",
			header:    "  Bearer   token  ",
			wantToken: "token",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := extractBearer(tt.header)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestParseRSAPublicKeyFromPem(t *testing.T) {
	tests := []struct {
		name        string
		setupPem    func() []byte
		wantErr     bool
		errContains string
	}{
		{
			name: "valid PKIX public key",
			setupPem: func() []byte {
				pubKeyBytes, _ := x509.MarshalPKIXPublicKey(testPublicKey)
				return pem.EncodeToMemory(&pem.Block{
					Type:  "PUBLIC KEY",
					Bytes: pubKeyBytes,
				})
			},
			wantErr: false,
		},
		{
			name: "valid PKCS1 public key",
			setupPem: func() []byte {
				pubKeyBytes := x509.MarshalPKCS1PublicKey(testPublicKey)
				return pem.EncodeToMemory(&pem.Block{
					Type:  "RSA PUBLIC KEY",
					Bytes: pubKeyBytes,
				})
			},
			wantErr: false,
		},
		{
			name: "invalid pem data",
			setupPem: func() []byte {
				return []byte("invalid pem data")
			},
			wantErr:     true,
			errContains: "failed to decode PEM block",
		},
		{
			name: "unknown pem type",
			setupPem: func() []byte {
				return pem.EncodeToMemory(&pem.Block{
					Type:  "UNKNOWN TYPE",
					Bytes: []byte("some data"),
				})
			},
			wantErr:     true,
			errContains: "unknown public key type",
		},
		{
			name: "invalid PKIX data",
			setupPem: func() []byte {
				return pem.EncodeToMemory(&pem.Block{
					Type:  "PUBLIC KEY",
					Bytes: []byte("invalid key data"),
				})
			},
			wantErr:     true,
			errContains: "failed to parse PKIX public key",
		},
		{
			name: "invalid PKCS1 data",
			setupPem: func() []byte {
				return pem.EncodeToMemory(&pem.Block{
					Type:  "RSA PUBLIC KEY",
					Bytes: []byte("invalid key data"),
				})
			},
			wantErr:     true,
			errContains: "asn1: structure error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pemData := tt.setupPem()
			pubKey, err := parseRSAPublicKeyFromPem(pemData)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.IsType(t, &rsa.PublicKey{}, pubKey)
		})
	}
}

func TestRS256Verifier_EdgeCases(t *testing.T) {
	t.Run("nil claims after verification", func(t *testing.T) {
		verifier, err := NewRS256Verifier(&config.JWTConfig{
			Enabled:       true,
			PublicKeyPath: testPublicKeyPath,
			Audience:      "",
			Issuer:        "",
		})
		require.NoError(t, err)

		// check uncorrected jwt token.
		authHeader := "Bearer invalid.token.structure"

		claims, err := verifier.VerifyBearer(authHeader)
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("different signing method", func(t *testing.T) {
		verifier, err := NewRS256Verifier(&config.JWTConfig{
			Enabled:       true,
			PublicKeyPath: testPublicKeyPath,
			Audience:      "",
			Issuer:        "",
		})
		require.NoError(t, err)

		// create token with different signing method.
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			Subject: "user123",
		})
		tokenString, _ := token.SignedString([]byte("secret"))

		authHeader := fmt.Sprintf("Bearer %s", tokenString)

		claims, err := verifier.VerifyBearer(authHeader)
		assert.Error(t, err)
		assert.Nil(t, claims)
		assert.Contains(t, err.Error(), "failed to parse token")
	})
}

// Help function to create invalid PEM file.
func createInvalidPemFile() string {
	tmpFile, err := os.CreateTemp("", "invalid_key_*.pem")
	if err != nil {
		panic(fmt.Sprintf("Failed to create temp file: %v", err))
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString("invalid pem content"); err != nil {
		panic(fmt.Sprintf("Failed to write to temp file: %v", err))
	}

	return tmpFile.Name()
}
