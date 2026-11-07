package huggingface

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
)

func TestImportGGUFVerifiesAndPreservesOriginal(t *testing.T) {
	data := []byte("GGUFtest weights")
	source := domain.HFSource{Repo: "org/model", Revision: strings.Repeat("a", 40), File: "model.gguf", Bytes: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	local := filepath.Join(t.TempDir(), "downloaded weights.gguf")
	if err := os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []bool{true, false} {
		dir := t.TempDir()
		if bad {
			if err := os.WriteFile(local, []byte("GGUFwrong weight"), 0600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(local, data, 0600); err != nil {
			t.Fatal(err)
		}
		err := Import(context.Background(), dir, source, local)
		target := filepath.Join(dir, CacheDir, FileName)
		if bad {
			if err == nil {
				t.Fatal("mismatched weights accepted")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("mismatched weights were published")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(local, []byte("original changed"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != string(data) {
			t.Fatalf("cache depends on the original file: %s, %v", got, err)
		}
		if err := Import(context.Background(), dir, source, local); err != nil {
			t.Fatalf("valid existing cache was not reused: %v", err)
		}
	}
}

func TestImportSnapshotCopiesOnlyPinnedFiles(t *testing.T) {
	local := t.TempDir()
	source := domain.HFSource{Repo: "org/model", Revision: strings.Repeat("a", 40)}
	for _, name := range []string{"config.json", "tokenizer.json", "model.safetensors"} {
		data := []byte("{}")
		a := domain.HFArtifact{Name: name, Bytes: int64(len(data))}
		if strings.HasSuffix(name, ".safetensors") {
			data = []byte("test weight data")
			a.Bytes = int64(len(data))
			a.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		} else {
			a.BlobID = fmt.Sprintf("%x", sha1.Sum(append([]byte("blob 2\x00"), data...)))
		}
		source.Bytes += a.Bytes
		source.Files = append(source.Files, a)
		if err := os.WriteFile(filepath.Join(local, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(local, "model.py"), []byte("not model data"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Import(context.Background(), dir, source, local); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, CacheDir))
	if len(entries) != len(source.Files) {
		t.Fatalf("unreviewed files were copied: %v", entries)
	}
	for _, file := range source.Files {
		if _, err := os.Stat(filepath.Join(dir, CacheDir, file.Name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, CacheDir, "config.json"), []byte("xx"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Import(context.Background(), dir, source, local); err == nil {
		t.Fatal("corrupt cache was replaced or accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Import(ctx, t.TempDir(), source, local); err == nil {
		t.Fatal("cancelled import succeeded")
	}
}
