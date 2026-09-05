package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

type Service struct {
	db         *sql.DB
	secret     []byte
	cookieName string
	maxAge     time.Duration
}

func NewService(db *sql.DB, secret, cookieName string, maxAge time.Duration) (*Service, error) {
	if db == nil || strings.TrimSpace(secret) == "" || strings.TrimSpace(cookieName) == "" {
		return nil, errors.New("database, session secret and cookie name are required")
	}
	return &Service{db: db, secret: []byte(secret), cookieName: cookieName, maxAge: maxAge}, nil
}

func (s *Service) Authenticate(ctx context.Context, cookieHeader string) (Identity, error) {
	value := cookieValue(cookieHeader, s.cookieName)
	userID, err := s.verifySession(value)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}
	identity, err := s.userByID(ctx, userID)
	if err != nil {
		return Identity{}, err
	}
	identity.Cookie = cookieHeader
	return identity, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (Identity, string, error) {
	var identity Identity
	var passwordHash string
	var active bool
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, display_name, password_hash, is_active
		   FROM users WHERE lower(email) = lower($1)`,
		strings.TrimSpace(email),
	).Scan(&identity.UserID, &identity.Email, &identity.DisplayName, &passwordHash, &active)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!active || !verifyPassword(password, passwordHash)) {
		return Identity{}, "", ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, "", fmt.Errorf("select login user: %w", err)
	}
	return identity, s.signSession(identity.UserID), nil
}

func (s *Service) CookieName() string {
	return s.cookieName
}

func (s *Service) MaxAgeSeconds() int {
	return int(s.maxAge / time.Second)
}

func (s *Service) userByID(ctx context.Context, userID int64) (Identity, error) {
	var identity Identity
	var active bool
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, email, display_name, is_active FROM users WHERE id = $1`,
		userID,
	).Scan(&identity.UserID, &identity.Email, &identity.DisplayName, &active)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !active {
		return Identity{}, ErrUnauthenticated
	}
	if err != nil {
		return Identity{}, fmt.Errorf("select session user: %w", err)
	}
	return identity, nil
}

func (s *Service) signSession(userID int64) string {
	payload, _ := json.Marshal(map[string]int64{"user_id": userID})
	value := base64.StdEncoding.EncodeToString(payload)
	timestamp := encodeTimestamp(time.Now().Unix())
	unsigned := value + "." + timestamp
	return unsigned + "." + s.signature(unsigned)
}

func (s *Service) verifySession(value string) (int64, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || !hmac.Equal([]byte(parts[2]), []byte(s.signature(parts[0]+"."+parts[1]))) {
		return 0, ErrUnauthenticated
	}
	timestamp, err := decodeTimestamp(parts[1])
	if err != nil || time.Since(time.Unix(timestamp, 0)) > s.maxAge {
		return 0, ErrUnauthenticated
	}
	payload, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, ErrUnauthenticated
	}
	var session struct {
		UserID int64 `json:"user_id"`
	}
	if json.Unmarshal(payload, &session) != nil || session.UserID < 1 {
		return 0, ErrUnauthenticated
	}
	return session.UserID, nil
}

func (s *Service) signature(value string) string {
	derived := sha1.Sum(append([]byte("itsdangerous.Signersigner"), s.secret...))
	mac := hmac.New(sha1.New, derived[:])
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeTimestamp(value int64) string {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(value))
	index := 0
	for index < len(raw)-1 && raw[index] == 0 {
		index++
	}
	return base64.RawURLEncoding.EncodeToString(raw[index:])
}

func decodeTimestamp(value string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 8 {
		return 0, ErrUnauthenticated
	}
	var padded [8]byte
	copy(padded[8-len(raw):], raw)
	return int64(binary.BigEndian.Uint64(padded[:])), nil
}

func cookieValue(header, name string) string {
	for _, part := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	salt, saltErr := base64.URLEncoding.DecodeString(parts[2])
	expected, digestErr := base64.URLEncoding.DecodeString(parts[3])
	if err != nil || saltErr != nil || digestErr != nil || iterations < 1 {
		return false
	}
	actual := pbkdf2.Key([]byte(password), salt, iterations, len(expected), sha256.New)
	return hmac.Equal(actual, expected)
}
