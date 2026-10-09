package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sobakintech/maitake/internal/accounts"
	"github.com/sobakintech/maitake/internal/config"
	"github.com/sobakintech/maitake/internal/provider/claude"
	"github.com/sobakintech/maitake/internal/provider/codex"
	"github.com/sobakintech/maitake/internal/proxy"
	"github.com/sobakintech/maitake/internal/resets"
	"github.com/sobakintech/maitake/internal/routing"
	"github.com/sobakintech/maitake/internal/secret"
	"github.com/sobakintech/maitake/internal/server"
	"github.com/sobakintech/maitake/internal/settings"
	"github.com/sobakintech/maitake/internal/store"
	"github.com/sobakintech/maitake/internal/usage"
	"github.com/sobakintech/maitake/internal/version"
	"github.com/sobakintech/maitake/web"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// upstreamClient talks to provider APIs. No overall timeout: responses stream for minutes.
func upstreamClient(proxyURL string) (*http.Client, error) {
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy-url: %w", err)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: tr}, nil
}

func every(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "maitake.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	sets, err := settings.Load(ctx, st)
	if err != nil {
		return err
	}
	sealer, err := secret.LoadOrCreateSealer(filepath.Join(cfg.DataDir, "keyring.key"))
	if err != nil {
		return fmt.Errorf("key encryption: %w", err)
	}

	admins, err := st.CountAdmins(ctx)
	if err != nil {
		return err
	}
	var setupToken string
	if admins == 0 {
		setupToken = secret.Token()
		slog.Warn("no admin account yet; open the dashboard and use this setup token", "token", setupToken)
	}

	client, err := upstreamClient(cfg.ProxyURL)
	if err != nil {
		return err
	}
	claudeProvider := claude.New(client, cfg.ProxyURL == "")
	claudeProvider.Cloak = func() claude.CloakSettings { return sets.Get().Claude }
	codexProvider := codex.New(client)
	mgr := accounts.New(st, codexProvider, func() accounts.Settings { return sets.Get().Pool }, claudeProvider, codexProvider)
	if err := mgr.Load(ctx); err != nil {
		return err
	}
	recorder := usage.NewRecorder(st)
	poller := resets.NewPoller(client, func() resets.Settings { return sets.Get().CodexResets })

	bg, cancelBG := context.WithCancel(context.Background())
	defer cancelBG()
	go recorder.Run(bg)
	go mgr.Run(ctx)
	go poller.Run(ctx)
	go every(ctx, time.Hour, func() {
		if err := st.PurgeExpiredSessions(ctx); err != nil {
			slog.Error("purge sessions", "err", err)
		}
		if days := sets.Get().UsageRetentionDays; days > 0 {
			if err := st.PruneUsage(ctx, time.Now().AddDate(0, 0, -days)); err != nil {
				slog.Error("prune usage", "err", err)
			}
		}
	})

	handler := server.New(server.Deps{
		Config:   cfg,
		Store:    st,
		Settings: sets,
		Accounts: mgr,
		Claude:   claudeProvider,
		Codex:    codexProvider,
		Proxy: proxy.New(st, mgr, claudeProvider, codexProvider, recorder,
			func() accounts.Settings { return sets.Get().Pool }, func() routing.Settings { return sets.Get().Routing }),
		Resets:     poller,
		Sealer:     sealer,
		UI:         web.Dist(),
		SetupToken: setupToken,
	})
	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "version", version.Version, "ui", web.Dist() != nil)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	cancelBG()
	recorder.Wait()
	return err
}
