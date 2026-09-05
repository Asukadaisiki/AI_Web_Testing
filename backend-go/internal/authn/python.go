package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PythonAuthenticator struct {
	baseURL    string
	httpClient *http.Client
}

func NewPythonAuthenticator(baseURL string, timeout time.Duration) (*PythonAuthenticator, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Python API URL must be an absolute HTTP URL")
	}
	return &PythonAuthenticator{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

func (a *PythonAuthenticator) Authenticate(
	ctx context.Context,
	cookieHeader string,
) (Identity, error) {
	if strings.TrimSpace(cookieHeader) == "" {
		return Identity{}, ErrUnauthenticated
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/auth/me", nil)
	if err != nil {
		return Identity{}, fmt.Errorf("create auth introspection request: %w", err)
	}
	request.Header.Set("Cookie", cookieHeader)
	response, err := a.httpClient.Do(request)
	if err != nil {
		return Identity{}, fmt.Errorf("call auth introspection: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return Identity{}, ErrUnauthenticated
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return Identity{}, fmt.Errorf(
			"auth introspection returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	var identity Identity
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&identity); err != nil {
		return Identity{}, fmt.Errorf("decode auth introspection response: %w", err)
	}
	if identity.UserID < 1 {
		return Identity{}, errors.New("auth introspection returned an invalid user")
	}
	identity.Cookie = cookieHeader
	return identity, nil
}
