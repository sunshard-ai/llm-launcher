package catalog

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
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
)

type catalogCache struct {
	Catalog   []byte `json:"catalog"`
	Signature []byte `json:"signature"`
}

func (c catalogCache) verify() (domain.Catalog, error) {
	key, err := x509.ParsePKIXPublicKey([]byte(llmlauncher.CatalogPublicKey()))
	if err != nil {
		return domain.Catalog{}, fmt.Errorf("catalog public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok || len(c.Catalog) > maxCatalog || !ed25519.Verify(pub, c.Catalog, c.Signature) {
		return domain.Catalog{}, fmt.Errorf("catalog signature is invalid")
	}
	return Decode(c.Catalog)
}

func cachePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "llm-launcher", "catalog-cache.json"), nil
}

func loadCache() (domain.Catalog, error) {
	path, err := cachePath()
	if err != nil {
		return domain.Catalog{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Catalog{}, err
	}
	defer file.Close()
	var c catalogCache
	if err := json.NewDecoder(io.LimitReader(file, 3<<20)).Decode(&c); err != nil {
		return domain.Catalog{}, err
	}
	return c.verify()
}

// Update stores the signed catalogue only after complete validation.
func Update(ctx context.Context) (domain.Catalog, error) {
	path, err := cachePath()
	if err != nil {
		return domain.Catalog{}, err
	}
	return update(ctx, publicCatalog, path)
}

func update(ctx context.Context, address, path string) (domain.Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, err := downloadCatalog(ctx, address, maxCatalog)
	if err != nil {
		return domain.Catalog{}, err
	}
	signature, err := downloadCatalog(ctx, address+".sig", ed25519.SignatureSize)
	if err != nil {
		return domain.Catalog{}, err
	}
	cache := catalogCache{Catalog: data, Signature: signature}
	c, err := cache.verify()
	if err != nil {
		return domain.Catalog{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return domain.Catalog{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.tmp")
	if err != nil {
		return domain.Catalog{}, err
	}
	defer os.Remove(tmp.Name())
	if err = json.NewEncoder(tmp).Encode(cache); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return domain.Catalog{}, err
	}
	if closeErr != nil {
		return domain.Catalog{}, closeErr
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return domain.Catalog{}, err
	}
	return c, nil
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
