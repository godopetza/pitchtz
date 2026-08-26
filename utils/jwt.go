package utils

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type AdminClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func IssueAdminToken(userID uuid.UUID, role string) (string, time.Time, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(EnvInt("JWT_ACCESS_TTL_MINUTES", 60)) * time.Minute)
	claims := AdminClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{"pitchtz-admin"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	return signed, expiresAt, err
}

func ParseAdminToken(raw string) (*AdminClaims, error) {
	secret, err := jwtSecret()
	if err != nil {
		return nil, err
	}
	claims := &AdminClaims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (interface{}, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer()),
		jwt.WithAudience("pitchtz-admin"),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

type OwnerClaims struct {
	jwt.RegisteredClaims
}

func IssueOwnerToken(userID uuid.UUID) (string, time.Time, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(EnvInt("JWT_ACCESS_TTL_MINUTES", 60)) * time.Minute)
	claims := OwnerClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{"pitchtz-owner"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	return signed, expiresAt, err
}

func ParseOwnerToken(raw string) (*OwnerClaims, error) {
	secret, err := jwtSecret()
	if err != nil {
		return nil, err
	}
	claims := &OwnerClaims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (interface{}, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer()),
		jwt.WithAudience("pitchtz-owner"),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

type ClientClaims struct {
	jwt.RegisteredClaims
}

func IssueClientToken(userID uuid.UUID) (string, time.Time, error) {
	secret, err := jwtSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(EnvInt("CLIENT_JWT_TTL_MINUTES", 43200)) * time.Minute) // 30 days
	claims := ClientClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer(),
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{"pitchtz-client"},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	return signed, expiresAt, err
}

func ParseClientToken(raw string) (*ClientClaims, error) {
	secret, err := jwtSecret()
	if err != nil {
		return nil, err
	}
	claims := &ClientClaims{}
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (interface{}, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer()),
		jwt.WithAudience("pitchtz-client"),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

func jwtSecret() ([]byte, error) {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return []byte(secret), nil
}

func jwtIssuer() string {
	if issuer := strings.TrimSpace(os.Getenv("JWT_ISSUER")); issuer != "" {
		return issuer
	}
	return "pitchtz-api"
}
