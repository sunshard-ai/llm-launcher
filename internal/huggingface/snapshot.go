package huggingface

import (
	"bytes"
	"context"
	"crypto/sha1" // Git blob identifiers for small repository files, not an authentication primitive.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"llmlauncher/internal/domain"
)

var weightsFile = regexp.MustCompile(`^model(?:-[0-9]{5}-of-[0-9]{5})?\.safetensors$`)

func snapshotFile(name string) bool {
	switch name {
	case "config.json", "generation_config.json", "model.safetensors.index.json",
		"tokenizer.json", "tokenizer_config.json", "special_tokens_map.json", "added_tokens.json",
		"tokenizer.model", "spiece.model", "vocab.json", "merges.txt", "chat_template.jinja":
		return true
	}
	return weightsFile.MatchString(name)
}

func validateSnapshot(s domain.HFSource) error {
	if !validRepo(s.Repo) || !commit.MatchString(s.Revision) || len(s.Files) > 256 ||
		s.File != "" || s.SHA256 != "" || s.Bytes <= 0 || s.Bytes > 1<<40 {
		return fmt.Errorf("invalid pinned Hugging Face snapshot")
	}
	var total int64
	var shards int
	seen := map[string]bool{}
	for _, a := range s.Files {
		weight := weightsFile.MatchString(a.Name)
		if !snapshotFile(a.Name) || seen[a.Name] || a.Bytes <= 0 || a.Bytes > 1<<40 ||
			(!weight && a.Bytes > 128<<20) ||
			!(digest.MatchString(a.SHA256) && a.BlobID == "" || !weight && a.SHA256 == "" && commit.MatchString(a.BlobID)) {
			return fmt.Errorf("invalid snapshot file %q", a.Name)
		}
		if weight && a.Name != "model.safetensors" {
			shards++
		}
		seen[a.Name] = true
		total += a.Bytes
	}
	if !seen["config.json"] || !(seen["tokenizer.json"] || seen["tokenizer.model"] ||
		seen["spiece.model"] || seen["vocab.json"] && seen["merges.txt"]) ||
		!(seen["model.safetensors"] && shards == 0 || !seen["model.safetensors"] && shards > 0 && seen["model.safetensors.index.json"]) ||
		total != s.Bytes {
		return fmt.Errorf("snapshot needs config, tokenizer and standard safetensors weights")
	}
	return nil
}

// ResolveSnapshot only selects data files. Python and arbitrary repository
// contents are excluded; the runtime uses the snapshot in offline mode.
func ResolveSnapshot(ctx context.Context, link, token string) (domain.HFSource, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.Host != "huggingface.co" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.HFSource{}, fmt.Errorf("use an HTTPS Hugging Face model repository link")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 && len(parts) != 4 {
		return domain.HFSource{}, fmt.Errorf("choose a repository link or /tree/<commit> for vLLM")
	}
	repo := strings.Join(parts[:2], "/")
	if !validRepo(repo) {
		return domain.HFSource{}, fmt.Errorf("invalid Hugging Face model repository")
	}
	var revision string
	if len(parts) == 4 {
		if parts[2] != "tree" || (parts[3] != "main" && !commit.MatchString(parts[3])) {
			return domain.HFSource{}, fmt.Errorf("choose main or a full commit SHA")
		}
		revision = parts[3]
	}
	endpoint := hub + "/api/models/" + repo
	if revision != "" {
		endpoint += "/revision/" + revision
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?blobs=true", nil)
	if err != nil {
		return domain.HFSource{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || req.URL.Scheme != "https" || req.URL.Host != "huggingface.co" || req.URL.User != nil {
			return fmt.Errorf("invalid Hub metadata redirect")
		}
		return nil
	}}
	res, err := client.Do(req)
	if err != nil {
		return domain.HFSource{}, fmt.Errorf("Hugging Face metadata request failed; check network and token")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return domain.HFSource{}, hubError(res.StatusCode)
	}
	var info struct {
		ID       string `json:"id"`
		SHA      string `json:"sha"`
		Pipeline string `json:"pipeline_tag"`
		Siblings []struct {
			Name string `json:"rfilename"`
			Size int64  `json:"size"`
			Blob string `json:"blobId"`
			LFS  struct {
				SHA256 string `json:"sha256"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&info); err != nil {
		return domain.HFSource{}, fmt.Errorf("invalid Hugging Face model metadata")
	}
	if (revision != "" && revision != "main" && revision != info.SHA) ||
		(info.Pipeline != "" && info.Pipeline != "text-generation") {
		return domain.HFSource{}, fmt.Errorf("expected the selected revision of a text-generation model")
	}
	s := domain.HFSource{Repo: info.ID, Revision: info.SHA}
	for _, f := range info.Siblings {
		if !snapshotFile(f.Name) {
			continue
		}
		artifact := domain.HFArtifact{Name: f.Name, Bytes: f.Size, SHA256: f.LFS.SHA256}
		if artifact.SHA256 == "" {
			artifact.BlobID = f.Blob
		}
		s.Bytes += artifact.Bytes
		s.Files = append(s.Files, artifact)
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Name < s.Files[j].Name })
	if len(s.Files) == 0 {
		return s, fmt.Errorf("no standard safetensors model files found")
	}
	return s, Validate(s)
}

func verifyArtifact(r io.Reader, out io.Writer, a domain.HFArtifact) error {
	var h hash.Hash
	var expected string
	if a.SHA256 != "" {
		h, expected = sha256.New(), a.SHA256
	} else {
		h, expected = sha1.New(), a.BlobID
		_, _ = fmt.Fprintf(h, "blob %d\x00", a.Bytes)
	}
	n, err := io.Copy(io.MultiWriter(h, out), io.LimitReader(r, a.Bytes+1))
	if err != nil || n != a.Bytes || hex.EncodeToString(h.Sum(nil)) != expected {
		return fmt.Errorf("download failed size or checksum verification")
	}
	return nil
}

// SnapshotIsAWQ reads the pinned config, so imports use the declared method
// rather than guessing from a repository name.
func SnapshotIsAWQ(ctx context.Context, source domain.HFSource, token string) (bool, error) {
	for _, config := range source.Files {
		if config.Name != "config.json" {
			continue
		}
		if config.Bytes > 4<<20 {
			return false, fmt.Errorf("model config.json exceeds 4 MiB")
		}
		source.File = config.Name
		req, err := request(ctx, http.MethodGet, source, token)
		if err != nil {
			return false, err
		}
		res, err := artifactClient().Do(req)
		if err != nil {
			return false, fmt.Errorf("could not fetch the pinned Hugging Face config.json")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return false, hubError(res.StatusCode)
		}
		var data bytes.Buffer
		if err := verifyArtifact(res.Body, &data, config); err != nil {
			return false, fmt.Errorf("config.json: %w", err)
		}
		var metadata struct {
			Quantization struct {
				Method string `json:"quant_method"`
			} `json:"quantization_config"`
		}
		if err := json.Unmarshal(data.Bytes(), &metadata); err != nil {
			return false, fmt.Errorf("invalid Hugging Face config.json")
		}
		return strings.EqualFold(metadata.Quantization.Method, "awq"), nil
	}
	return false, fmt.Errorf("pinned model has no config.json")
}
