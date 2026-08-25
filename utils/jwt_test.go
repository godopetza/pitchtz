package utils

import (
	"testing"

	"github.com/google/uuid"
)

func TestAdminTokenRoundTrip(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-only-secret-with-more-than-32-characters")
	t.Setenv("JWT_ISSUER", "pitchtz-test")
	t.Setenv("JWT_ACCESS_TTL_MINUTES", "5")
	userID := uuid.New()
	token, expiresAt, err := IssueAdminToken(userID, "super_admin")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if expiresAt.IsZero() {
		t.Fatal("token expiry was not set")
	}
	claims, err := ParseAdminToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if claims.Subject != userID.String() || claims.Role != "super_admin" {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestAdminTokenRequiresStrongSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "too-short")
	if _, _, err := IssueAdminToken(uuid.New(), "super_admin"); err == nil {
		t.Fatal("expected a short JWT secret to be rejected")
	}
}
