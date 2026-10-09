package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/config"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/proxy"
	"github.com/sobakintech/maitake/internal/resets"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/settings"
	"github.com/sobakintech/maitake/internal/store"
	"github.com/sobakintech/maitake/internal/version"
)

type Deps struct {
	Config     *config.Config
	Store      *store.Store
	Settings   *settings.Store
	Accounts   *accounts.Manager
	Claude     *claude.Provider
	Codex      *codex.Provider
	Proxy      *proxy.Handler
	Resets     *resets.Poller
	Sealer     *secret.Sealer
	UI         fs.FS
	SetupToken string
}

type Server struct {
	cfg      *config.Config
	store    *store.Store
	settings *settings.Store
	accounts *accounts.Manager
	claude   *claude.Provider
	codex    *codex.Provider
	resets   *resets.Poller
	sealer   *secret.Sealer
	oauth    oauthSessions
	logins   *failureLimiter

	mu         sync.Mutex
	setupToken string // non-empty until the first admin is created
}

func New(d Deps) http.Handler {
	s := &Server{cfg: d.Config, store: d.Store, settings: d.Settings, accounts: d.Accounts, claude: d.Claude,
		codex: d.Codex, resets: d.Resets, sealer: d.Sealer, setupToken: d.SetupToken,
		logins: newFailureLimiter(d.Config.Auth.LoginRateLimit.Attempts, d.Config.Auth.LoginRateLimit.Window)}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})

	if d.Proxy != nil {
		d.Proxy.ClientIP = func(r *http.Request) string { return s.clientAddr(r).String() }
		d.Proxy.Mount(r)
	}

	r.Route("/api", func(r chi.Router) {
		if d.Proxy != nil {
			d.Proxy.MountOpenRouter(r)
		}
		r.Get("/session", s.getSession)
		r.Post("/setup", s.setup)
		r.Post("/login", s.login)
		r.Post("/logout", s.logout)

		r.With(s.requireKey).Get("/me", s.me)
		r.Get("/quota/meta", s.quotaMeta)
		r.Get("/quota", s.publicQuota)

		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireAdmin)
			r.Get("/users", s.listUsers)
			r.Post("/users", s.createUser)
			r.Get("/users/{id}", s.getUser)
			r.Patch("/users/{id}", s.updateUser)
			r.Delete("/users/{id}", s.deleteUser)
			r.Post("/users/{id}/keys", s.createKey)
			r.Get("/keys", s.listAllKeys)
			r.Get("/keys/{id}", s.getKey)
			r.Get("/keys/{id}/secret", s.keySecret)
			r.Post("/keys/{id}/rotate", s.rotateKey)
			r.Delete("/key-copies", s.clearKeySecrets)
			r.Get("/models", s.listModels)
			r.Post("/models/resolve", s.resolveModel)
			r.Post("/keys", s.createOwnedKey)
			r.Patch("/keys/{id}", s.updateKey)
			r.Delete("/keys/{id}", s.deleteKey)

			r.Get("/accounts", s.listAccounts)
			r.Post("/accounts/oauth/start", s.startOAuth)
			r.Post("/accounts/oauth/complete", s.completeOAuth)
			r.Post("/accounts/device/start", s.startDevice)
			r.Get("/accounts/device/{session}", s.deviceStatus)
			r.Delete("/accounts/device/{session}", s.cancelDevice)
			r.Post("/accounts/import", s.importAccount)
			r.Post("/accounts/refresh-all", s.refreshAllAccounts)
			r.Patch("/accounts/{id}", s.updateAccount)
			r.Delete("/accounts/{id}", s.deleteAccount)
			r.Post("/accounts/{id}/refresh", s.refreshAccount)
			r.Put("/accounts/{id}/web-session", s.setWebSession)
			r.Post("/accounts/{id}/redeem", s.redeemAccount)

			r.Get("/usage", s.usage)
			r.Get("/audit", s.listAudit)
			r.Delete("/audit", s.clearAudit)
			r.Get("/audit/{id}", s.getAudit)
			r.Delete("/audit/{id}", s.deleteAudit)

			r.Get("/settings", s.getSettings)
			r.Put("/settings", s.putSettings)
			r.Get("/profiles", s.listProfiles)
			r.Post("/profiles", s.createProfile)
			r.Post("/profiles/preview", s.previewProfile)
			r.Patch("/profiles/{id}", s.updateProfile)
			r.Delete("/profiles/{id}", s.deleteProfile)
			r.Get("/codex-resets", s.codexResets)
			r.Post("/codex-resets/refresh", s.refreshCodexResets)
		})

		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	})

	if d.UI != nil {
		r.NotFound(spa(d.UI))
	}
	return r
}

// spa serves built assets and falls back to index.html for client-side routes.
func spa(ui fs.FS) http.HandlerFunc {
	files := http.FileServerFS(ui)
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(ui, name); err != nil {
			http.ServeFileFS(w, r, ui, "index.html")
			return
		}
		if strings.HasPrefix(name, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError maps store errors to responses, logging anything unexpected.
func writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
