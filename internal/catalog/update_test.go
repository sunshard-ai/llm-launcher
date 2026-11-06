package catalog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"llmlauncher"
	"llmlauncher/internal/domain"
)

func TestPublishedCatalogSignature(t *testing.T) {
	sig, err := os.ReadFile("../../catalog.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	u, err := defaultUpdater()
	if err != nil {
		t.Fatal(err)
	}
	cache := catalogCache{Catalog: []byte(llmlauncher.EmbeddedCatalog()), Signature: sig}
	if _, err := cache.verify(u.key); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRefreshScheduleAndFallback(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	base, err := Decode([]byte(llmlauncher.EmbeddedCatalog()))
	if err != nil {
		t.Fatal(err)
	}
	newer := base
	newer.Updated = catalogTime(base).Add(time.Hour).Format(time.RFC3339)
	signed := func(c domain.Catalog) catalogCache {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return catalogCache{Catalog: data, Signature: ed25519.Sign(private, data)}
	}
	var remote atomic.Pointer[catalogCache]
	old, fresh := signed(base), signed(newer)
	remote.Store(&fresh)
	var unavailable atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if unavailable.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		c := remote.Load()
		switch r.URL.Path {
		case "/catalog.json":
			_, _ = w.Write(c.Catalog)
		case "/catalog.json.sig":
			_, _ = w.Write(c.Signature)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u := updater{address: server.URL + "/catalog.json", path: filepath.Join(t.TempDir(), "catalog-cache.json"), key: pub}
	ctx := context.Background()
	if c, err := u.current(); err != nil || c.Updated != base.Updated {
		t.Fatalf("first launch should use the bundled catalog: %v", err)
	}
	if c, changed, err := u.check(ctx, true); err != nil || !changed || c.Updated != newer.Updated {
		t.Fatalf("first automatic check did not install the signed update: %v", err)
	}
	if _, changed, err := u.check(ctx, true); err != nil || changed || requests.Load() != 2 {
		t.Fatalf("automatic refresh was not throttled: %v (requests %d)", err, requests.Load())
	}
	remote.Store(&old)
	if c, changed, err := u.check(ctx, false); err != nil || changed || c.Updated != newer.Updated || requests.Load() != 4 {
		t.Fatalf("manual check did not run or accepted a rollback: %v", err)
	}
	if c, err := u.current(); err != nil || c.Updated != newer.Updated {
		t.Fatalf("saved catalog did not survive the rollback attempt: %v", err)
	}
	tampered := catalogCache{Catalog: append(bytes.Clone(fresh.Catalog), ' '), Signature: fresh.Signature}
	remote.Store(&tampered)
	if _, _, err := u.check(ctx, false); err == nil {
		t.Fatal("invalid signature was accepted")
	}
	unavailable.Store(true)
	yesterday := time.Now().Add(-checkInterval - time.Minute)
	if err := os.Chtimes(u.path+".check", yesterday, yesterday); err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.check(ctx, true); err == nil || requests.Load() != 7 {
		t.Fatalf("expired automatic check did not attempt the network: %v", err)
	}
	if _, _, err := u.check(ctx, true); err != nil || requests.Load() != 7 {
		t.Fatalf("network failure was not throttled: %v (requests %d)", err, requests.Load())
	}
	if c, err := u.current(); err != nil || c.Updated != newer.Updated {
		t.Fatalf("failed update changed the active catalog: %v", err)
	}
	stale := base
	stale.Updated = catalogTime(base).Add(-time.Hour).Format(time.RFC3339)
	if err := signed(stale).save(u.path); err != nil {
		t.Fatal(err)
	}
	if c, err := u.current(); err != nil || c.Updated != base.Updated {
		t.Fatalf("an old cache took priority over the bundled catalog: %v", err)
	}
}

func TestAutomaticRefreshSkipsWhileAnotherProcessChecks(t *testing.T) {
	u := updater{address: "http://127.0.0.1:0/catalog.json", path: filepath.Join(t.TempDir(), "catalog-cache.json")}
	file, err := os.OpenFile(u.path+".check", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if locked, err := tryCatalogLock(file); !locked || err != nil {
		t.Fatalf("could not hold the first check lock: %v", err)
	}
	if _, changed, err := u.check(context.Background(), true); changed || err != nil {
		t.Fatalf("background check should skip an active check: %v", err)
	}
}
