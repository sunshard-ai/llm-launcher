package catalog

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"llmlauncher"
	"llmlauncher/internal/domain"
)

const (
	publicCatalog = "https://raw.githubusercontent.com/sunshard-ai/llm-launcher/main/catalog.json"
	maxCatalog    = 2 << 20
	checkInterval = 24 * time.Hour
)

type updater struct {
	address, path string
	key           ed25519.PublicKey
}

type catalogCache struct {
	Catalog   []byte `json:"catalog"`
	Signature []byte `json:"signature"`
}

func defaultUpdater() (updater, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return updater{}, err
	}
	key, err := x509.ParsePKIXPublicKey([]byte(llmlauncher.CatalogPublicKey()))
	if err != nil {
		return updater{}, fmt.Errorf("catalog public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return updater{}, fmt.Errorf("catalog public key must use Ed25519")
	}
	return updater{
		address: publicCatalog,
		path:    filepath.Join(dir, "llm-launcher", "catalog-cache.json"),
		key:     pub,
	}, nil
}

func (c catalogCache) verify(key ed25519.PublicKey) (domain.Catalog, error) {
	if len(c.Catalog) > maxCatalog || !ed25519.Verify(key, c.Catalog, c.Signature) {
		return domain.Catalog{}, fmt.Errorf("catalog signature is invalid")
	}
	cat, err := Decode(c.Catalog)
	if err != nil {
		return domain.Catalog{}, err
	}
	if catalogTime(cat).IsZero() {
		return domain.Catalog{}, fmt.Errorf("signed catalog has an invalid updated timestamp")
	}
	return cat, nil
}

func catalogTime(c domain.Catalog) time.Time {
	if ts, err := time.Parse(time.RFC3339, c.Updated); err == nil {
		return ts
	}
	ts, _ := time.Parse(time.DateOnly, c.Updated)
	return ts
}

func (u updater) loadCache() (domain.Catalog, error) {
	file, err := os.Open(u.path)
	if err != nil {
		return domain.Catalog{}, err
	}
	defer file.Close()
	var c catalogCache
	if err := json.NewDecoder(io.LimitReader(file, 3<<20)).Decode(&c); err != nil {
		return domain.Catalog{}, err
	}
	return c.verify(u.key)
}

func (u updater) current() (domain.Catalog, error) {
	bundled, err := Decode([]byte(llmlauncher.EmbeddedCatalog()))
	if err != nil {
		return domain.Catalog{}, err
	}
	if cached, err := u.loadCache(); err == nil && catalogTime(cached).After(catalogTime(bundled)) {
		return cached, nil
	}
	return bundled, nil
}

// Refresh is a best-effort check for interactive startup. Call it in the background.
func Refresh(ctx context.Context) {
	if u, err := defaultUpdater(); err == nil {
		_, _, _ = u.check(ctx, true)
	}
}

// Update forces a check and reports whether a newer signed catalog was installed.
func Update(ctx context.Context) (domain.Catalog, bool, error) {
	u, err := defaultUpdater()
	if err != nil {
		return domain.Catalog{}, false, err
	}
	return u.check(ctx, false)
}

func (u updater) check(ctx context.Context, automatic bool) (domain.Catalog, bool, error) {
	timeout := 20 * time.Second
	if automatic {
		timeout = 5 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(u.path), 0700); err != nil {
		return domain.Catalog{}, false, err
	}
	check, err := os.OpenFile(u.path+".check", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return domain.Catalog{}, false, err
	}
	defer check.Close()
	for {
		if err := requestCtx.Err(); err != nil {
			return domain.Catalog{}, false, err
		}
		locked, err := tryCatalogLock(check)
		if err != nil {
			return domain.Catalog{}, false, err
		}
		if locked {
			break
		}
		if automatic {
			return domain.Catalog{}, false, nil
		}
		select {
		case <-requestCtx.Done():
			return domain.Catalog{}, false, requestCtx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if automatic {
		info, err := check.Stat()
		if err != nil {
			return domain.Catalog{}, false, err
		}
		if age := time.Since(info.ModTime()); info.Size() > 0 && age >= 0 && age < checkInterval {
			return domain.Catalog{}, false, nil
		}
	}
	defer func() {
		if ctx.Err() == nil {
			// Keep failed network attempts on the same schedule as successful checks.
			_, _ = check.WriteAt([]byte{1}, 0)
		}
	}()
	return u.update(requestCtx)
}

func (u updater) update(ctx context.Context) (domain.Catalog, bool, error) {
	current, err := u.current()
	if err != nil {
		return domain.Catalog{}, false, err
	}
	data, err := downloadCatalog(ctx, u.address, maxCatalog)
	if err != nil {
		return domain.Catalog{}, false, err
	}
	signature, err := downloadCatalog(ctx, u.address+".sig", ed25519.SignatureSize)
	if err != nil {
		return domain.Catalog{}, false, err
	}
	cache := catalogCache{Catalog: data, Signature: signature}
	next, err := cache.verify(u.key)
	if err != nil {
		return domain.Catalog{}, false, err
	}
	if !catalogTime(next).After(catalogTime(current)) {
		return current, false, nil
	}
	if err := ctx.Err(); err != nil {
		return domain.Catalog{}, false, err
	}
	if err := cache.save(u.path); err != nil {
		return domain.Catalog{}, false, err
	}
	return next, true, nil
}

func (c catalogCache) save(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = json.NewEncoder(tmp).Encode(c); err == nil {
		err = tmp.Sync()
	}
	if err := errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func downloadCatalog(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public catalog returned HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("catalog response exceeds the allowed size")
	}
	return b, nil
}
