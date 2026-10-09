package config

import (
	"net/netip"
	"testing"
	"time"
)

func TestLoadExample(t *testing.T) {
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Auth.LoginRateLimit.Window != 15*time.Minute || c.Auth.LoginRateLimit.Attempts != 10 {
		t.Fatalf("rate limit not parsed: %+v", c.Auth.LoginRateLimit)
	}
	if c.IsTrustedProxy(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("no proxies should be trusted by default")
	}
}
