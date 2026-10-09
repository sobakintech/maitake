package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/store"
)

const (
	sessionCookie = "maitake_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type ctxKey int

const (
	adminKey ctxKey = iota
	keyOwnerKey
)

func (s *Server) setupPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupToken != ""
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"setupRequired": s.setupPending(), "admin": nil}
	if a := s.sessionAdmin(r); a != nil {
		resp["admin"] = a
	}
	writeJSON(w, http.StatusOK, resp)
}

// throttled rejects the request if this client has too many recent failed login/setup attempts.
func (s *Server) throttled(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := s.clientAddr(r).String()
	if blocked, wait := s.logins.blocked(key); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return key, true
	}
	return key, false
}

type credentials struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !readJSON(w, r, &body) {
		return
	}
	limitKey, blocked := s.throttled(w, r)
	if blocked {
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Username) > 64 {
		writeError(w, http.StatusBadRequest, "username must be 1-64 characters")
		return
	}
	if err := secret.ValidatePassword(body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	token := s.setupToken
	s.mu.Unlock()
	if token == "" {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Token), []byte(token)) != 1 {
		s.logins.fail(limitKey)
		writeError(w, http.StatusForbidden, "invalid setup token")
		return
	}

	hash, err := secret.HashPassword(body.Password)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	admin, err := s.store.CreateFirstAdmin(r.Context(), body.Username, hash)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}
	if err != nil {
		writeStoreError(w, r, err)
		return
	}

	s.mu.Lock()
	s.setupToken = ""
	s.mu.Unlock()

	if err := s.startSession(w, r, admin.ID); err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"admin": admin})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if !readJSON(w, r, &body) {
		return
	}
	limitKey, blocked := s.throttled(w, r)
	if blocked {
		return
	}
	admin, err := s.store.AdminByUsername(r.Context(), strings.TrimSpace(body.Username))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeStoreError(w, r, err)
		return
	}
	if admin == nil {
		secret.BurnPasswordCheck(body.Password)
		s.logins.fail(limitKey)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !secret.CheckPassword(admin.PasswordHash, body.Password) {
		s.logins.fail(limitKey)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.logins.reset(limitKey)
	if err := s.startSession(w, r, admin.ID); err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"admin": admin})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), secret.Hash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, adminID int64) error {
	token := secret.Token()
	if err := s.store.CreateSession(r.Context(), secret.Hash(token), adminID, sessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: s.isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Server) sessionAdmin(r *http.Request) *store.Admin {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	a, err := s.store.AdminBySession(r.Context(), secret.Hash(c.Value))
	if err != nil {
		return nil
	}
	return a
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cookie auth: refuse cross-site state changes on top of SameSite=Lax.
		if r.Method != http.MethodGet && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		a := s.sessionAdmin(r)
		if a == nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKey, a)))
	})
}

// requireKey authenticates a client API key from the headers used by OpenAI, Anthropic and Gemini clients.
func (s *Server) requireKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientKey(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "missing API key")
			return
		}
		owner, err := s.store.AuthenticateKey(r.Context(), secret.Hash(key))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		if err != nil {
			writeStoreError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyOwnerKey, owner)))
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	o := r.Context().Value(keyOwnerKey).(*store.KeyOwner)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{"name": o.OwnerName},
		"key":  map[string]any{"name": o.Key.Name, "prefix": o.Key.Prefix, "expiresAt": o.Key.ExpiresAt},
	})
}
