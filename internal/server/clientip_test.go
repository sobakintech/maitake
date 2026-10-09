package server

import (
	"net/http/httptest"
	"testing"

	"github.com/sobakintech/maitake/internal/config"
)

func TestClientAddr(t *testing.T) {
	cfg := config.Default()
	cfg.Server.TrustedProxies = []string{"10.0.0.0/8"}
	loaded, err := config.ForTesting(cfg.Server.TrustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: loaded}
	cases := []struct {
		remote, xff, want string
	}{
		{"198.51.100.1:5000", "1.2.3.4", "198.51.100.1"},
		{"10.0.0.2:5000", "1.2.3.4", "1.2.3.4"},
		{"10.0.0.2:5000", "6.6.6.6, 1.2.3.4, 10.0.0.3", "1.2.3.4"},
		{"10.0.0.2:5000", "", "10.0.0.2"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := s.clientAddr(r).String(); got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}
