package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

// The renewal date is only on claude.ai's own billing endpoint, which rejects OAuth tokens;
// it takes the browser session cookie instead. claude.ai sits behind Cloudflare, so requests
// use a Chrome TLS handshake.

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

var (
	browserOnce   sync.Once
	browserClient *http.Client
)

func chromeClient() *http.Client {
	browserOnce.Do(func() {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		tr := &http.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				raw, err := dialer.DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				host, _, _ := net.SplitHostPort(addr)
				spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
				if err != nil {
					raw.Close()
					return nil, err
				}
				// Keep Chrome's handshake but speak HTTP/1.1, which net/http handles natively.
				for _, ext := range spec.Extensions {
					if alpn, ok := ext.(*utls.ALPNExtension); ok {
						alpn.AlpnProtocols = []string{"http/1.1"}
					}
				}
				conn := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloCustom)
				if err := conn.ApplyPreset(&spec); err != nil {
					raw.Close()
					return nil, err
				}
				if err := conn.HandshakeContext(ctx); err != nil {
					raw.Close()
					return nil, err
				}
				return conn, nil
			},
			IdleConnTimeout: 90 * time.Second,
		}
		browserClient = &http.Client{Transport: tr, Timeout: 30 * time.Second}
	})
	return browserClient
}

// WebBase is claude.ai; overridable for tests.
var WebBase = "https://claude.ai"

// Field names that may hold the next charge date, most specific first.
var renewalKeys = []string{"next_charge_date", "next_billing_date", "next_payment_date", "next_invoice_date",
	"renewal_date", "renews_at", "current_period_end", "billing_period_end", "period_end"}

// webRenewal reads the next charge date from claude.ai's subscription details.
func (p *Provider) webRenewal(ctx context.Context, session, orgUUID string) (*time.Time, error) {
	if orgUUID == "" {
		return nil, errors.New("organization unknown")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		WebBase+"/api/organizations/"+orgUUID+"/subscription_details", nil)
	if err != nil {
		return nil, err
	}
	cookie := strings.TrimSpace(session)
	if !strings.Contains(cookie, "=") {
		cookie = "sessionKey=" + cookie
	}
	h := req.Header
	h.Set("Cookie", cookie)
	h.Set("User-Agent", browserUA)
	h.Set("Accept", "application/json")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Referer", WebBase+"/settings/billing")
	h.Set("anthropic-client-platform", "web_claude_ai")

	client := p.client
	if p.fingerprint != nil { // direct connection: no outbound proxy configured
		client = chromeClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), `"type":"error"`):
		return nil, errors.New("the claude.ai session is expired or belongs to another account")
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("this claude.ai session can't see this account's organization")
	case resp.StatusCode != http.StatusOK:
		if strings.Contains(string(body), "Just a moment") || strings.Contains(string(body), "cf-") {
			return nil, errors.New("claude.ai blocked the request (Cloudflare)")
		}
		return nil, fmt.Errorf("claude.ai returned %d", resp.StatusCode)
	}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, errors.New("claude.ai returned something other than JSON")
	}
	found := map[string]any{}
	collectKeys(data, found)
	for _, k := range renewalKeys {
		if t := parseDate(found[k]); t != nil {
			return t, nil
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	slog.Warn("claude.ai subscription details had no renewal date", "fields", strings.Join(keys, ","))
	return nil, errors.New("claude.ai didn't include a renewal date")
}

// collectKeys flattens every key in a JSON document, keeping the first value seen for each.
func collectKeys(v any, out map[string]any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if _, seen := out[k]; !seen && val != nil {
				out[k] = val
			}
			collectKeys(val, out)
		}
	case []any:
		for _, it := range t {
			collectKeys(it, out)
		}
	}
}

func parseDate(v any) *time.Time {
	switch t := v.(type) {
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02"} {
			if d, err := time.Parse(layout, t); err == nil {
				d = d.UTC()
				return &d
			}
		}
	case float64:
		if t > 1e12 {
			t /= 1000
		}
		if t > 1e9 {
			d := time.Unix(int64(t), 0).UTC()
			return &d
		}
	}
	return nil
}
