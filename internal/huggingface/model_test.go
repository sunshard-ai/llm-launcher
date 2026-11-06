package huggingface

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
)

func TestGGUFSourceAndVerification(t *testing.T) {
	source, err := parseLink("https://huggingface.co/org/repo/blob/main/path/model.gguf")
	if err != nil || source.Repo != "org/repo" || source.File != "path/model.gguf" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	for _, link := range []string{
		"https://huggingface.co.evil.test/org/repo/blob/main/model.gguf",
		"https://user@huggingface.co/org/repo/blob/main/model.gguf",
		"https://huggingface.co/org/repo/blob/main/../model.gguf",
		"https://huggingface.co/org/repo/blob/main/model-00001-of-00002.gguf",
	} {
		if _, err := parseLink(link); err == nil {
			t.Errorf("accepted %s", link)
		}
	}
	data := "GGUFtesting"
	source.Revision = strings.Repeat("a", 40)
	source.Bytes, source.SHA256 = int64(len(data)), fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	var out bytes.Buffer
	if err := Validate(source); err != nil {
		t.Fatal(err)
	}
	if err := verifyGGUF(strings.NewReader(data), source, &out); err != nil || out.String() != data {
		t.Fatalf("verification failed: %v", err)
	}
	for _, invalid := range []string{"NOTGtesting", data + "x", data[:len(data)-1], "GGUFaltered"} {
		if err := verifyGGUF(strings.NewReader(invalid), source, io.Discard); err == nil {
			t.Errorf("accepted corrupted artifact %q", invalid)
		}
	}
}

func TestSnapshotRejectsExecutableAndUnpinnedFiles(t *testing.T) {
	config := "{}"
	blob := fmt.Sprintf("%x", sha1.Sum([]byte("blob 2\x00"+config)))
	s := domain.HFSource{Repo: "org/repo", Revision: strings.Repeat("a", 40), Bytes: 14, Files: []domain.HFArtifact{
		{Name: "config.json", Bytes: 2, BlobID: blob},
		{Name: "tokenizer.json", Bytes: 2, BlobID: blob},
		{Name: "model.safetensors", Bytes: 10, SHA256: strings.Repeat("b", 64)},
	}}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
	if err := verifyArtifact(strings.NewReader(config), io.Discard, s.Files[0]); err != nil {
		t.Fatal(err)
	}
	s.Files[2] = domain.HFArtifact{Name: "model.py", Bytes: 10, BlobID: blob}
	if err := Validate(s); err == nil {
		t.Fatal("snapshot accepted executable code")
	}
}
