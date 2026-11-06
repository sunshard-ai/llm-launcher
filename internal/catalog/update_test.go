package catalog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"llmlauncher"
)

func TestUpdateKeepsLastVerifiedCatalog(t *testing.T) {
	sig, err := os.ReadFile("../../catalog.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	data := llmlauncher.EmbeddedCatalog()
	var corrupt atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog.json":
			_, _ = w.Write([]byte(data))
			if corrupt.Load() {
				_, _ = w.Write([]byte(" "))
			}
		case "/catalog.json.sig":
			_, _ = w.Write(sig)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "catalog-cache.json")
	address := server.URL + "/catalog.json"
	for range 2 {
		if c, err := update(context.Background(), address, cache); err != nil || len(c.Models) == 0 {
			t.Fatalf("verified update failed: %v", err)
		}
	}
	before, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	corrupt.Store(true)
	if _, err := update(context.Background(), address, cache); err == nil {
		t.Fatal("accepted data with an invalid signature")
	}
	after, err := os.ReadFile(cache)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed update changed the last verified catalog: %v", err)
	}
}
