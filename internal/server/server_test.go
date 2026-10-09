package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sobakintech/maitake/internal/config"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/settings"
	"github.com/sobakintech/maitake/internal/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *client) do(method, path string, body any, headers map[string]string) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.base+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func newTestServer(t *testing.T) (*client, *client) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Default()
	cfg.Auth.LoginRateLimit.Attempts = 3
	sets, err := settings.Load(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := secret.LoadOrCreateSealer(filepath.Join(t.TempDir(), "keyring.key"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(Deps{Config: cfg, Store: st, Settings: sets, Sealer: sealer, SetupToken: "setup-token"}))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	admin := &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}
	anon := &client{t: t, base: ts.URL, http: &http.Client{}}
	return admin, anon
}

func TestAdminAndKeyFlow(t *testing.T) {
	admin, anon := newTestServer(t)

	if code, _ := admin.do("GET", "/api/admin/users", nil, nil); code != 401 {
		t.Fatalf("unauthenticated admin call: got %d", code)
	}
	if code, _ := admin.do("POST", "/api/setup", map[string]string{"token": "wrong", "username": "root", "password": "correct horse battery"}, nil); code != 403 {
		t.Fatalf("bad setup token: got %d", code)
	}
	if code, body := admin.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "root", "password": "correct horse battery"}, nil); code != 200 {
		t.Fatalf("setup: %d %v", code, body)
	}
	if code, _ := anon.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "evil", "password": "correct horse battery"}, nil); code != 409 {
		t.Fatalf("second setup: got %d", code)
	}

	code, body := admin.do("POST", "/api/admin/users", map[string]string{"name": "alice"}, nil)
	if code != 201 {
		t.Fatalf("create user: %d %v", code, body)
	}
	userID := int(body["user"].(map[string]any)["id"].(float64))

	code, body = admin.do("POST", "/api/admin/users/"+strconv.Itoa(userID)+"/keys", map[string]string{"name": "laptop"}, nil)
	if code != 201 {
		t.Fatalf("create key: %d %v", code, body)
	}
	secretKey := body["secret"].(string)
	keyID := int(body["key"].(map[string]any)["id"].(float64))

	bearer := map[string]string{"Authorization": "Bearer " + secretKey}
	if code, body := anon.do("GET", "/api/me", nil, bearer); code != 200 || body["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("me with key: %d %v", code, body)
	}
	if code, _ := anon.do("GET", "/api/me", nil, map[string]string{"X-Api-Key": secretKey}); code != 200 {
		t.Fatalf("me with x-api-key: %d", code)
	}

	if code, _ := admin.do("PATCH", "/api/admin/keys/"+strconv.Itoa(keyID), map[string]any{"disabled": true}, nil); code != 200 {
		t.Fatalf("disable key: %d", code)
	}
	if code, _ := anon.do("GET", "/api/me", nil, bearer); code != 401 {
		t.Fatalf("disabled key accepted: %d", code)
	}
	admin.do("PATCH", "/api/admin/keys/"+strconv.Itoa(keyID), map[string]any{"disabled": false}, nil)

	if code, _ := admin.do("PATCH", "/api/admin/users/"+strconv.Itoa(userID), map[string]any{"disabled": true}, nil); code != 200 {
		t.Fatalf("disable user: %d", code)
	}
	if code, _ := anon.do("GET", "/api/me", nil, bearer); code != 401 {
		t.Fatalf("key of disabled user accepted: %d", code)
	}

	if code, _ := admin.do("DELETE", "/api/admin/users/"+strconv.Itoa(userID), nil, map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Fatalf("cross-site delete: got %d", code)
	}
	if code, _ := admin.do("DELETE", "/api/admin/users/"+strconv.Itoa(userID), nil, nil); code != 204 {
		t.Fatalf("delete user: %d", code)
	}
	if code, _ := admin.do("GET", "/api/admin/users/"+strconv.Itoa(userID), nil, nil); code != 404 {
		t.Fatalf("deleted user still present: %d", code)
	}

	admin.do("POST", "/api/logout", nil, nil)
	if code, _ := admin.do("GET", "/api/admin/users", nil, nil); code != 401 {
		t.Fatalf("after logout: got %d", code)
	}
	if code, _ := admin.do("POST", "/api/login", map[string]string{"username": "ROOT", "password": "correct horse battery"}, nil); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code, _ := anon.do("POST", "/api/login", map[string]string{"username": "root", "password": "nope"}, nil); code != 401 {
		t.Fatalf("bad login: %d", code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	admin, anon := newTestServer(t)
	admin.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "root", "password": "correct horse battery"}, nil)
	for i := 0; i < 3; i++ {
		if code, _ := anon.do("POST", "/api/login", map[string]string{"username": "root", "password": "wrong"}, nil); code != 401 {
			t.Fatalf("attempt %d: got %d", i, code)
		}
	}
	if code, _ := anon.do("POST", "/api/login", map[string]string{"username": "root", "password": "correct horse battery"}, nil); code != 429 {
		t.Fatalf("expected lockout, got %d", code)
	}
	// X-Forwarded-For is ignored without trusted proxies, so spoofing doesn't bypass the lockout.
	if code, _ := anon.do("POST", "/api/login", map[string]string{"username": "root", "password": "correct horse battery"},
		map[string]string{"X-Forwarded-For": "203.0.113.9"}); code != 429 {
		t.Fatalf("spoofed XFF bypassed lockout: %d", code)
	}
}

func TestAdminOwnedKeys(t *testing.T) {
	admin, anon := newTestServer(t)
	admin.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "root", "password": "correct horse battery"}, nil)

	code, body := admin.do("POST", "/api/admin/keys", map[string]string{"name": "mine"}, nil)
	if code != 201 {
		t.Fatalf("create admin key: %d %v", code, body)
	}
	if body["key"].(map[string]any)["adminId"] == nil || body["key"].(map[string]any)["userId"] != nil {
		t.Fatalf("expected admin-owned key: %v", body["key"])
	}
	secretKey := body["secret"].(string)
	if code, body := anon.do("GET", "/api/me", nil, map[string]string{"Authorization": "Bearer " + secretKey}); code != 200 ||
		body["user"].(map[string]any)["name"] != "root" {
		t.Fatalf("admin key auth: %d %v", code, body)
	}

	_, body = admin.do("POST", "/api/admin/users", map[string]string{"name": "bob"}, nil)
	bobID := body["user"].(map[string]any)["id"].(float64)
	if code, _ := admin.do("POST", "/api/admin/keys", map[string]any{"name": "for bob", "userId": bobID}, nil); code != 201 {
		t.Fatalf("create user key via keys endpoint: %d", code)
	}

	code, body = admin.do("GET", "/api/admin/keys", nil, nil)
	keys := body["keys"].([]any)
	if code != 200 || len(keys) != 2 {
		t.Fatalf("list keys: %d %v", code, body)
	}
	owners := map[string]string{}
	for _, k := range keys {
		m := k.(map[string]any)
		owners[m["name"].(string)] = m["ownerType"].(string) + ":" + m["ownerName"].(string)
	}
	if owners["mine"] != "admin:root" || owners["for bob"] != "user:bob" {
		t.Fatalf("unexpected owners: %v", owners)
	}
}

func TestKeyExpiryPatch(t *testing.T) {
	admin, _ := newTestServer(t)
	admin.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "root", "password": "correct horse battery"}, nil)
	_, body := admin.do("POST", "/api/admin/keys", map[string]string{"name": "k"}, nil)
	id := strconv.Itoa(int(body["key"].(map[string]any)["id"].(float64)))

	future := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	code, body := admin.do("PATCH", "/api/admin/keys/"+id, map[string]any{"expiresAt": future}, nil)
	if code != 200 || body["key"].(map[string]any)["expiresAt"] != future.Format(time.RFC3339) {
		t.Fatalf("set expiry: %d %v", code, body)
	}
	if code, body := admin.do("PATCH", "/api/admin/keys/"+id, map[string]any{"name": "renamed"}, nil); code != 200 ||
		body["key"].(map[string]any)["expiresAt"] == nil {
		t.Fatalf("absent expiresAt should leave it unchanged: %v", body)
	}
	if code, _ := admin.do("PATCH", "/api/admin/keys/"+id, map[string]any{"expiresAt": time.Now().Add(-time.Hour)}, nil); code != 400 {
		t.Fatalf("past expiry accepted: %d", code)
	}
	if code, body := admin.do("PATCH", "/api/admin/keys/"+id, map[string]any{"expiresAt": nil}, nil); code != 200 ||
		body["key"].(map[string]any)["expiresAt"] != nil {
		t.Fatalf("clear expiry: %d %v", code, body)
	}
}

func TestKeyCopies(t *testing.T) {
	admin, anon := newTestServer(t)
	admin.do("POST", "/api/setup", map[string]string{"token": "setup-token", "username": "root", "password": "correct horse battery"}, nil)
	_, body := admin.do("POST", "/api/admin/keys", map[string]string{"name": "k"}, nil)
	original := body["secret"].(string)
	id := strconv.Itoa(int(body["key"].(map[string]any)["id"].(float64)))
	if body["key"].(map[string]any)["copyable"] != true {
		t.Fatalf("new key should be copyable: %v", body["key"])
	}
	if code, body := admin.do("GET", "/api/admin/keys/"+id+"/secret", nil, nil); code != 200 || body["secret"] != original {
		t.Fatalf("copy: %d %v", code, body)
	}
	if code, _ := anon.do("GET", "/api/admin/keys/"+id+"/secret", nil, nil); code != 401 {
		t.Fatalf("secret must need an admin session: %d", code)
	}

	_, body = admin.do("POST", "/api/admin/keys/"+id+"/rotate", nil, nil)
	rotated := body["secret"].(string)
	if rotated == original {
		t.Fatal("rotate returned the same secret")
	}
	if code, _ := anon.do("GET", "/api/me", nil, map[string]string{"Authorization": "Bearer " + original}); code != 401 {
		t.Fatalf("old secret still works after rotate: %d", code)
	}
	if code, _ := anon.do("GET", "/api/me", nil, map[string]string{"Authorization": "Bearer " + rotated}); code != 200 {
		t.Fatalf("rotated secret rejected: %d", code)
	}

	if code, body := admin.do("DELETE", "/api/admin/key-copies", nil, nil); code != 200 || body["cleared"].(float64) != 1 {
		t.Fatalf("clear copies: %d %v", code, body)
	}
	if code, _ := admin.do("GET", "/api/admin/keys/"+id+"/secret", nil, nil); code != 404 {
		t.Fatalf("copy should be gone: %d", code)
	}
}
