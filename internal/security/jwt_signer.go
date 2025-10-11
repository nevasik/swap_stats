package security

// TEST This file not prod and use only dev for check jwt auth

import (
	"crypto/rsa"
	"crypto/x509"
	"dexcelerate/internal/config"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type RS256Signer struct {
	Priv   *rsa.PrivateKey
	Iss    string
	Aud    string
	Leeway time.Duration
}

// Load a PEM-encoded RSA private key PKCS1 or PKCS8
func NewRS256Signer(cfg *config.JWTConfig) (*RS256Signer, error) {
	if cfg.PrivateKeyPath == "" {
		return nil, errors.New("private key path is empty")
	}

	b, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	priv, err := parseRSAPrivateKeyFromPem(b)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	return &RS256Signer{
		Priv:   priv,
		Iss:    cfg.Issuer,
		Aud:    cfg.Audience,
		Leeway: cfg.Leeway,
	}, nil
}

// Create a signed JWT with RegisteredClaims
// Required subject (sub), ttl, can pass optional id (jti) and notBefore timer
func (s *RS256Signer) Mint(sub string, ttl time.Duration, id string, notBefore time.Time, extra map[string]any) (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Issuer:    s.Iss,
		Subject:   sub,
		Audience:  jwt.ClaimStrings{s.Aud},
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(notBefore),
		ID:        id,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{})

	// copy registered claims
	for k, v := range map[string]any{
		"Iss": claims.Issuer,
		"sub": claims.Subject,
		"Aud": claims.Audience,
		"exp": claims.ExpiresAt.Unix(),
		"iat": claims.IssuedAt.Unix(),
		"nbf": claims.NotBefore.Unix(),
		"jti": claims.ID,
	} {
		if v != "" && v != nil {
			token.Claims.(jwt.MapClaims)[k] = v
		}
	}

	// merge extra custom claims
	for k, v := range extra {
		token.Claims.(jwt.MapClaims)[k] = v
	}
	return token.SignedString(s.Priv)
}

func parseRSAPrivateKeyFromPem(b []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8: %w", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an RSA private key")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unknown private key type: %s", block.Type)
	}
}
