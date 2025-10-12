package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"dexcelerate/internal/config"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// --- helpers ---
func genRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)

	require.NoError(t, err)

	return key
}

func writePKCS1PEM(t *testing.T, key *rsa.PrivateKey, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}

	f, err := os.Create(path)
	defer f.Close()

	require.NoError(t, err)
	require.NoError(t, pem.Encode(f, block))

	return path
}

func writePKCS8PEM(t *testing.T, key *rsa.PrivateKey, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	der, err := x509.MarshalPKCS8PrivateKey(key)

	require.NoError(t, err)

	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	f, err := os.Create(path)
	defer f.Close()

	require.NoError(t, err)
	require.NoError(t, pem.Encode(f, block))

	return path
}

func parseWithPublicKey(t *testing.T, tokenStr string, pub *rsa.PublicKey, opts ...jwt.ParserOption) (*jwt.Token, *jwt.RegisteredClaims) {
	t.Helper()

	rc := &jwt.RegisteredClaims{}
	tok, err := jwt.ParseWithClaims(tokenStr, rc, func(token *jwt.Token) (any, error) {
		return pub, nil
	}, opts...)

	require.NoError(t, err)
	require.True(t, tok.Valid)

	return tok, rc
}

// --- tests ---
func TestNewRS256Signer_LoadsPKCS1AndPKCS8(t *testing.T) {
	dir := t.TempDir()
	key := genRSAKey(t)

	pkcs1 := writePKCS1PEM(t, key, dir, "key_pkcs1.pem")
	pkcs8 := writePKCS8PEM(t, key, dir, "key_pkcs8.pem")

	for _, path := range []string{pkcs1, pkcs8} {
		cfg := &config.JWTConfig{
			PrivateKeyPath: path,
			Issuer:         "issuer-X",
			Audience:       "aud-Y",
			Leeway:         0,
		}
		s, err := NewRS256Signer(cfg)

		require.NoError(t, err, "must load %s", path)
		require.NotNil(t, s.Priv)
		require.Equal(t, cfg.Issuer, s.Iss)
		require.Equal(t, cfg.Audience, s.Aud)
	}
}

func TestMintAndVerifyClaims(t *testing.T) {
	dir := t.TempDir()
	key := genRSAKey(t)
	path := writePKCS1PEM(t, key, dir, "key.pem")

	cfg := &config.JWTConfig{
		PrivateKeyPath: path,
		Issuer:         "swap-stats-auth",
		Audience:       "swap-stats",
	}
	signer, err := NewRS256Signer(cfg)

	require.NoError(t, err)

	now := time.Now()
	extra := map[string]any{
		"role":  "user",
		"scope": []string{"read", "write"},
	}

	tokenStr, err := signer.Mint("user-123", 2*time.Minute, "id-777", now.Add(-1*time.Second), extra)

	require.NoError(t, err)
	require.NotEmpty(t, tokenStr)

	// verify using public key
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithAudience(cfg.Audience),
		jwt.WithLeeway(0),
	}
	tok, rc := parseWithPublicKey(t, tokenStr, &key.PublicKey, opts...)

	// header algorithm
	require.Equal(t, jwt.SigningMethodRS256, tok.Method)

	// registered claims
	require.Equal(t, cfg.Issuer, rc.Issuer)
	require.Equal(t, "user-123", rc.Subject)
	require.Contains(t, rc.Audience, cfg.Audience)
	require.NotNil(t, rc.ExpiresAt)
	require.NotNil(t, rc.IssuedAt)
	require.NotNil(t, rc.NotBefore)
	require.Equal(t, "id-777", rc.ID)
	require.WithinDuration(t, now, rc.IssuedAt.Time, 2*time.Second)
	require.True(t, rc.ExpiresAt.Time.After(now))

	// custom claims present
	var m jwt.MapClaims
	_, err = jwt.ParseWithClaims(tokenStr, &m, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	}, opts...)

	require.NoError(t, err)
	require.Equal(t, "user", m["role"])
	require.ElementsMatch(t, []any{"read", "write"}, m["scope"].([]any))
}

func TestMint_NotBeforeInFuture_FailsValidationAtCurrentTime(t *testing.T) {
	dir := t.TempDir()
	key := genRSAKey(t)
	path := writePKCS1PEM(t, key, dir, "key.pem")

	cfg := &config.JWTConfig{
		PrivateKeyPath: path,
		Issuer:         "issuer",
		Audience:       "aud",
	}
	signer, err := NewRS256Signer(cfg)

	require.NoError(t, err)

	nbf := time.Now().Add(10 * time.Second) // ещё не наступило
	tokenStr, err := signer.Mint("sub-1", time.Minute, "jti-1", nbf, nil)

	require.NoError(t, err)

	// Проверяем при текущем времени без leeway — должно упасть по nbf
	_, err = jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithAudience(cfg.Audience),
		jwt.WithLeeway(0),
	)

	require.Error(t, err, "token should not be valid before nbf")
}

func TestNewRS256Signer_Errors(t *testing.T) {
	// nil config
	_, err := NewRS256Signer(nil)

	require.Error(t, err)

	// missing file
	cfg := &config.JWTConfig{PrivateKeyPath: filepath.Join(t.TempDir(), "missing.pem")}
	_, err = NewRS256Signer(cfg)

	require.Error(t, err)

	// invalid PEM
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.pem")

	require.NoError(t, os.WriteFile(bad, []byte("not-a-pem"), 0o600))

	cfg = &config.JWTConfig{PrivateKeyPath: bad}
	_, err = NewRS256Signer(cfg)

	require.Error(t, err)
}
