package authn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPythonAuthenticatorForwardsCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/api/v1/auth/me" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Cookie") != "session=signed-cookie" {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"id":7,"email":"owner@example.com","display_name":"Owner"}`,
		))
	}))
	defer server.Close()

	authenticator, err := NewPythonAuthenticator(server.URL+"/api/v1", time.Second)
	if err != nil {
		t.Fatalf("NewPythonAuthenticator() error = %v", err)
	}
	identity, err := authenticator.Authenticate(context.Background(), "session=signed-cookie")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.UserID != 7 || identity.Cookie != "session=signed-cookie" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestPythonAuthenticatorMapsUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	authenticator, err := NewPythonAuthenticator(server.URL, time.Second)
	if err != nil {
		t.Fatalf("NewPythonAuthenticator() error = %v", err)
	}
	_, err = authenticator.Authenticate(context.Background(), "session=expired")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
	}
}
