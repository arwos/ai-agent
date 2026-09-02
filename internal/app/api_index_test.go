package app

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/web"

	frontend "github.com/arwos/ai-agent/web"
)

func TestApiIndexServesEmbeddedSPAAndStaticCachePolicy(t *testing.T) {
	a := &App{}

	root := serveIndex(t, a, "/")
	if root.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", root.Code, http.StatusOK)
	}
	if !strings.Contains(root.Body.String(), "id=\"root\"") {
		t.Fatal("root response does not contain React application")
	}

	index := serveIndex(t, a, "/workspace")
	if index.Code != http.StatusOK {
		t.Fatalf("SPA route status = %d, want %d", index.Code, http.StatusOK)
	}
	if cache := index.Header().Get("Cache-Control"); cache != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("index cache-control = %q, want no-store policy", cache)
	}

	entries, err := fs.ReadDir(frontend.Browser, "assets")
	if err != nil {
		t.Fatal(err)
	}
	asset := ""
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") {
			asset = "assets/" + entry.Name()
			break
		}
	}
	if asset == "" {
		t.Fatal("embedded frontend has no JavaScript asset")
	}

	static := serveIndex(t, a, "/"+asset)
	if static.Code != http.StatusOK {
		t.Fatalf("static asset status = %d, want %d", static.Code, http.StatusOK)
	}
	if cache := static.Header().Get("Cache-Control"); cache != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("static cache-control = %q", cache)
	}
}

func TestApiIndexDoesNotFallbackForMissingAPIOrAsset(t *testing.T) {
	a := &App{}
	for _, requestPath := range []string{"/api/missing", "/missing.js"} {
		response := serveIndex(t, a, requestPath)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want %d", requestPath, response.Code, http.StatusNotFound)
		}
	}
}

func serveIndex(t *testing.T, a *App, requestPath string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response := httptest.NewRecorder()
	a.ApiIndex(web.NewCtx(response, request))
	return response
}
