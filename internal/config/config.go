package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config holds bootstrap settings only. Users, keys, provider accounts and
// runtime settings live in the database and are managed from the dashboard.
type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
		// TrustedProxies lists IPs/CIDRs whose X-Forwarded-For / X-Forwarded-Proto headers are honored.
		TrustedProxies []string `yaml:"trusted-proxies"`
	} `yaml:"server"`
	DataDir string `yaml:"data-dir"`
	Auth    struct {
		LoginRateLimit struct {
			Attempts int           `yaml:"attempts"`
			Window   time.Duration `yaml:"window"`
		} `yaml:"login-rate-limit"`
	} `yaml:"auth"`
	// ProxyURL routes all upstream provider traffic through an HTTP(S) or SOCKS5 proxy.
	ProxyURL string `yaml:"proxy-url"`

	trusted []netip.Prefix
}

func Default() *Config {
	c := &Config{DataDir: "data"}
	c.Server.Host = "127.0.0.1"
	c.Server.Port = 8317
	c.Auth.LoginRateLimit.Attempts = 10
	c.Auth.LoginRateLimit.Window = 15 * time.Minute
	return c
}

// Load reads path if it exists; a missing file yields defaults.
func Load(path string) (*Config, error) {
	c := Default()
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := yaml.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if err := c.applyEnv(); err != nil {
		return nil, err
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return nil, fmt.Errorf("invalid server.port %d", c.Server.Port)
	}
	for _, p := range c.Server.TrustedProxies {
		prefix, err := parsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("invalid server.trusted-proxies entry %q: %w", p, err)
		}
		c.trusted = append(c.trusted, prefix)
	}
	if c.Auth.LoginRateLimit.Window <= 0 {
		c.Auth.LoginRateLimit.Window = 15 * time.Minute
	}
	return c, nil
}

// applyEnv lets environment variables override the file, for container deployments:
// MAITAKE_HOST, MAITAKE_PORT (or PORT), MAITAKE_DATA_DIR, MAITAKE_TRUSTED_PROXIES
// (comma-separated) and MAITAKE_PROXY_URL.
func (c *Config) applyEnv() error {
	if v := os.Getenv("MAITAKE_HOST"); v != "" {
		c.Server.Host = v
	}
	port := os.Getenv("MAITAKE_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("invalid port %q", port)
		}
		c.Server.Port = n
	}
	if v := os.Getenv("MAITAKE_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := os.Getenv("MAITAKE_TRUSTED_PROXIES"); v != "" {
		c.Server.TrustedProxies = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Server.TrustedProxies = append(c.Server.TrustedProxies, p)
			}
		}
	}
	if v, ok := os.LookupEnv("MAITAKE_PROXY_URL"); ok {
		c.ProxyURL = v
	}
	return nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func (c *Config) Addr() string {
	return net.JoinHostPort(c.Server.Host, strconv.Itoa(c.Server.Port))
}

func (c *Config) IsTrustedProxy(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range c.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ForTesting returns defaults with the given trusted proxies parsed.
func ForTesting(trusted []string) (*Config, error) {
	c := Default()
	for _, p := range trusted {
		prefix, err := parsePrefix(p)
		if err != nil {
			return nil, err
		}
		c.trusted = append(c.trusted, prefix)
	}
	return c, nil
}
