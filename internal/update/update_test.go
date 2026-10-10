package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseNotes(t *testing.T) {
	body := "## [0.2.0](https://github.com/sobakintech/maitake/compare/v0.1.0...v0.2.0) (2026-10-12)\n\n\n" +
		"### Features\n\n" +
		"* **ui:** show available updates ([1a2b3c4](https://github.com/sobakintech/maitake/commit/1a2b3c4d))\n" +
		"* add [Kimi](https://kimi.com) accounts ([5d6e7f8](https://github.com/x/y/commit/5d6e7f8)), closes [#12](https://github.com/x/y/issues/12)\n\n" +
		"### Bug Fixes\n\n" +
		"* keep cooldowns across restarts ([9a8b7c6](https://github.com/x/y/commit/9a8b7c6))\n\n" +
		"### Miscellaneous\n"
	want := []Section{
		{Title: "Features", Items: []string{"ui: show available updates", "add Kimi accounts"}},
		{Title: "Bug Fixes", Items: []string{"keep cooldowns across restarts"}},
	}
	if got := ParseNotes(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestCheckerFindsNewerReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"tag_name":"v0.10.0","html_url":"u10","body":"### Features\n\n* ten"},
			{"tag_name":"v0.11.0-rc.1","prerelease":true},
			{"tag_name":"v0.9.1","html_url":"u91","body":"* fix"},
			{"tag_name":"v0.9.0","html_url":"u90"},
			{"tag_name":"v0.8.0","html_url":"u80"}
		]`))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewrite{srv.URL, client.Transport}

	snap := NewChecker(client, "0.9.0", DefaultSettings).Check(context.Background())
	if snap.Error != "" {
		t.Fatal(snap.Error)
	}
	if snap.Latest == nil || snap.Latest.Version != "0.10.0" {
		t.Fatalf("latest = %+v", snap.Latest)
	}
	var newer []string
	for _, r := range snap.Newer {
		newer = append(newer, r.Version)
	}
	if !reflect.DeepEqual(newer, []string{"0.10.0", "0.9.1"}) {
		t.Fatalf("newer = %v", newer)
	}

	dev := NewChecker(client, "dev-1a2b3c4", DefaultSettings).Check(context.Background())
	if dev.UpdateAvailable() || dev.Latest == nil {
		t.Fatalf("dev build: %+v", dev)
	}
}

type rewrite struct {
	target string
	next   http.RoundTripper
}

func (rw rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = "http", rw.target[len("http://"):]
	return rw.next.RoundTrip(r)
}
