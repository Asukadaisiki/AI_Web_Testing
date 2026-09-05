package authn

import (
	"testing"
	"time"
)

func TestSignatureMatchesItsDangerousTimestampSigner(t *testing.T) {
	service := &Service{secret: []byte("test-secret")}
	if got := service.signature("eyJ1c2VyX2lkIjoxfQ.YWJj"); got != "pfrWTwY7x3l1IOts3w-WGMLviRQ" {
		t.Fatalf("signature = %q", got)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	service := &Service{
		secret:     []byte("test-secret"),
		cookieName: "session",
		maxAge:     time.Hour,
	}
	value := service.signSession(42)
	userID, err := service.verifySession(value)
	if err != nil {
		t.Fatalf("verifySession() error = %v", err)
	}
	if userID != 42 {
		t.Fatalf("user id = %d, want 42", userID)
	}
}

func TestVerifyPasswordMatchesPythonFormat(t *testing.T) {
	encoded := "pbkdf2_sha256$120000$MDEyMzQ1Njc4OWFiY2RlZg==$pzIiMOsBL9wbBPhrfqJk57WQn4VLMXjbg9pOxAMm34k="
	if !verifyPassword("password", encoded) {
		t.Fatal("verifyPassword rejected a Python-compatible digest")
	}
	if verifyPassword("wrong", encoded) {
		t.Fatal("verifyPassword accepted a wrong password")
	}
}
