package security

import (
	"crypto/rsa"
	"crypto/x509"
	"dexcelerate/internal/config"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrNoBearerToken = errors.New("authorization header must be: Bearer <token>")
)

// Check JWT RS256 with audience/issuer and allow for shifting hours - Leeway
type RS256Verifier struct {
	PubKey *rsa.PublicKey
	Aud    string
	Iss    string
	Leeway time.Duration
}

// Load pub_key and parsing, audience/issuer can leave empty - not check
func NewRS256Verifier(cfg *config.JWTConfig) (*RS256Verifier, error) {
	b, err := os.ReadFile(cfg.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key: %w", err)
	}

	pub, err := parseRSAPublicKeyFromPem(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return &RS256Verifier{
		PubKey: pub,
		Aud:    cfg.Audience,
		Iss:    cfg.Issuer,
		Leeway: cfg.Leeway,
	}, nil
}

// Apply header Authorization and validation token
func (v *RS256Verifier) VerifyBearer(authHeader string) (any, error) {
	tokenStr, err := extractBearer(authHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bearer token: %w", err)
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), // only RS256
		jwt.WithLeeway(v.Leeway),
		jwt.WithIssuedAt(),           // check iat if exists
		jwt.WithExpirationRequired(), // check exp if exists
	}

	if v.Aud != "" {
		opts = append(opts, jwt.WithAudience(v.Aud))
	}
	if v.Iss != "" {
		opts = append(opts, jwt.WithIssuer(v.Iss))
	}

	claims := &jwt.RegisteredClaims{}
	if _, err = jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		return v.PubKey, nil
	}, opts...); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return claims, nil
}

func extractBearer(h string) (string, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return "", ErrNoBearerToken
	}

	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", ErrNoBearerToken
	}

	return strings.TrimSpace(parts[1]), nil
}

func parseRSAPublicKeyFromPem(b []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PKIX public key: %w", err)
		}
		rsaPub, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("failed to parse RSA public key")
		}
		return rsaPub, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unknown public key type: %s", block.Type)
	}
}
