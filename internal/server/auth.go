package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
	pool       *pgxpool.Pool
	adminToken string
	cookieName string
	sessionTTL time.Duration
}

func NewAuth(pool *pgxpool.Pool, cfg ServerConfig) *Auth {
	ttl := 8 * time.Hour
	if strings.TrimSpace(cfg.Auth.SessionTTL) != "" {
		if parsed, err := time.ParseDuration(cfg.Auth.SessionTTL); err == nil && parsed > 0 {
			ttl = parsed
		}
	}
	name := strings.TrimSpace(cfg.Auth.CookieName)
	if name == "" {
		name = "probe_session"
	}
	return &Auth{
		pool:       pool,
		adminToken: strings.TrimSpace(cfg.Security.AdminToken),
		cookieName: name,
		sessionTTL: ttl,
	}
}

func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}

	var userID, hash, role string
	err := a.pool.QueryRow(context.Background(),
		`SELECT id, password_hash, role FROM users WHERE username=$1`, body.Username).Scan(&userID, &hash, &role)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := randomBase64(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}
	tokenHash := sha256Hex(token)
	expiresAt := time.Now().Add(a.sessionTTL)

	// cleanup expired then insert
	_, _ = a.pool.Exec(context.Background(), `DELETE FROM sessions WHERE expires_at < now()`)
	_, err = a.pool.Exec(context.Background(),
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "role": role})
}

func (a *Auth) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(a.cookieName); err == nil && cookie != nil {
		tokenHash := sha256Hex(cookie.Value)
		_, _ = a.pool.Exec(context.Background(), `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Auth) HandleMe(w http.ResponseWriter, r *http.Request) {
	principal, err := a.AuthenticateRequest(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"principal":     principal,
	})
}

func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.AuthenticateRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *Auth) RequireAdmin(next http.Handler) http.Handler {
	return a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := PrincipalFromContext(r.Context())
		if p.Role != "admin" {
			writeError(w, http.StatusForbidden, "admin required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *Auth) AuthenticateRequest(r *http.Request) (Principal, error) {
	// cookie auth
	if a.pool != nil {
		if cookie, err := r.Cookie(a.cookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
			tokenHash := sha256Hex(cookie.Value)
			var sub, username, role string
			err := a.pool.QueryRow(context.Background(),
				`SELECT u.id, u.username, u.role FROM sessions s
				 JOIN users u ON u.id = s.user_id
				 WHERE s.token_hash=$1 AND s.expires_at > now()`, tokenHash).Scan(&sub, &username, &role)
			if err == nil {
				return Principal{Subject: sub, Username: username, Role: role}, nil
			}
		}
	}
	// X-Admin-Token fallback
	if a.adminToken != "" {
		token := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
		if token != "" && subtleConstantCompare(token, a.adminToken) {
			return Principal{Subject: "admin-token", Username: "admin-token", Role: "admin"}, nil
		}
		// Bearer fallback
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			bt := strings.TrimSpace(authHeader[7:])
			if bt != "" && subtleConstantCompare(bt, a.adminToken) {
				return Principal{Subject: "admin-token", Username: "admin-token", Role: "admin"}, nil
			}
		}
	}
	return Principal{}, errors.New("no valid session")
}

func SeedUser(ctx context.Context, pool *pgxpool.Pool, username, password, role string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)
		 ON CONFLICT (username) DO UPDATE SET password_hash=$2, role=$3, updated_at=now()`,
		username, string(hash), role)
	return err
}

type principalContextKey struct{}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	value := ctx.Value(principalContextKey{})
	principal, ok := value.(Principal)
	return principal, ok
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomBase64(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func subtleConstantCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	diff := byte(0)
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
