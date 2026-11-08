package codex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sobakintech/maitake/internal/provider"
)

func TestListModels(t *testing.T) {
	var version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version = r.URL.Query().Get("client_version")
		w.Write([]byte(`{"models":[
			{"slug":"gpt-reserve","visibility":"hide"},
			{"slug":"gpt-6-luna","visibility":"list"},
			{"slug":"gpt-5.5"},
			{"visibility":"list"}]}`))
	}))
	defer srv.Close()
	p := New(srv.Client())
	p.BackendBase = srv.URL
	list, err := p.ListModels(context.Background(), provider.Credentials{AccessToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if version != listClientVersion {
		t.Errorf("client_version = %q", version)
	}
	if len(list) != 3 || !list[0].Preview || list[1].Preview || list[1].ID != "gpt-6-luna" || list[2].Preview {
		t.Errorf("list = %+v", list)
	}
}
