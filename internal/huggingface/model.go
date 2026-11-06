// Package huggingface resolves and downloads single GGUF artifacts from the Hub.
package huggingface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"llmlauncher/internal/domain"
)

const CacheDir = "hf-model"
const FileName = "weights.gguf"
const hub = "https://huggingface.co"

var (
	part   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
	commit = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
	shard  = regexp.MustCompile(`(?i)-[0-9]{5}-of-[0-9]{5}\.gguf$`)
)

func validPart(s string) bool {
	return part.MatchString(s) && !strings.Contains(s, "..")
}

func validFile(file string) bool {
	for _, s := range strings.Split(file, "/") {
		if !validPart(s) {
			return false
		}
	}
	return len(file) <= 768 && strings.HasSuffix(strings.ToLower(file), ".gguf") && !shard.MatchString(file)
}

func validRepo(repo string) bool {
	org, name, ok := strings.Cut(repo, "/")
	return ok && validPart(org) && validPart(name)
}

func Validate(s domain.HFSource) error {
	if len(s.Files) != 0 {
		return validateSnapshot(s)
	}
	if !validRepo(s.Repo) || !commit.MatchString(s.Revision) || !validFile(s.File) ||
		!digest.MatchString(s.SHA256) || s.Bytes <= 4 || s.Bytes > 1<<40 {
		return fmt.Errorf("invalid Hugging Face source; expected a pinned single GGUF file, size and SHA-256")
	}
	return nil
}

func resolveURL(s domain.HFSource) string {
	return hub + "/" + s.Repo + "/resolve/" + s.Revision + "/" + s.File
}

func parseLink(link string) (domain.HFSource, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "https" || u.Host != "huggingface.co" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return domain.HFSource{}, fmt.Errorf("use an HTTPS Hugging Face file link without credentials, query or fragment")
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 5 || (parts[2] != "blob" && parts[2] != "resolve") ||
		(parts[3] != "main" && !commit.MatchString(parts[3])) {
		return domain.HFSource{}, fmt.Errorf("copy a Hugging Face GGUF file link with main or a full commit, for example /org/repo/blob/main/model.gguf")
	}
	s := domain.HFSource{Repo: strings.Join(parts[:2], "/"), Revision: parts[3], File: strings.Join(parts[4:], "/")}
	if !validRepo(s.Repo) || !validFile(s.File) {
		return domain.HFSource{}, fmt.Errorf("choose a single GGUF file; split weights are not supported")
	}
	return s, nil
}

func request(ctx context.Context, method string, s domain.HFSource, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, resolveURL(s), nil)
	if err == nil && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, err
}

func hubError(code int) error {
	if code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusNotFound {
		return fmt.Errorf("Hugging Face returned HTTP %d; check the file link, model access and read token", code)
	}
	return fmt.Errorf("Hugging Face returned HTTP %d", code)
}

func Resolve(ctx context.Context, link, token string) (domain.HFSource, error) {
	s, err := parseLink(link)
	if err != nil {
		return s, err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Follow only Hugging Face repo moves. A redirect to the storage CDN
	// already contains metadata and never receives the token during resolution.
	for range 4 {
		req, err := request(ctx, http.MethodHead, s, token)
		if err != nil {
			return s, fmt.Errorf("could not prepare Hugging Face request")
		}
		res, err := client.Do(req)
		if err != nil {
			return s, fmt.Errorf("Hugging Face metadata request failed; check network and token")
		}
		res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 400 {
			return s, hubError(res.StatusCode)
		}
		if sha := res.Header.Get("X-Repo-Commit"); sha != "" {
			if s.Revision != "main" && s.Revision != sha {
				return s, fmt.Errorf("Hugging Face returned an unexpected commit")
			}
			s.Revision = sha
			s.SHA256 = strings.Trim(res.Header.Get("X-Linked-ETag"), "\"")
			s.Bytes, _ = strconv.ParseInt(res.Header.Get("X-Linked-Size"), 10, 64)
			if s.SHA256 == "" {
				s.SHA256 = strings.Trim(res.Header.Get("ETag"), "\"")
				s.Bytes = res.ContentLength
			}
			return s, Validate(s)
		}
		next, err := res.Location()
		if err != nil {
			break
		}
		moved, err := parseLink(next.String())
		if err != nil || moved.File != s.File || moved.Revision != s.Revision {
			break
		}
		s = moved
	}
	return s, fmt.Errorf("Hugging Face did not return a pinned GGUF file with SHA-256 and size")
}

func verifyGGUF(r io.Reader, s domain.HFSource, out io.Writer) error {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil || string(header) != "GGUF" {
		return fmt.Errorf("downloaded file has no GGUF header")
	}
	hash := sha256.New()
	w := io.MultiWriter(hash, out)
	if _, err := w.Write(header); err != nil {
		return err
	}
	n, err := io.Copy(w, io.LimitReader(r, s.Bytes-3))
	if err != nil || n+4 != s.Bytes || hex.EncodeToString(hash.Sum(nil)) != s.SHA256 {
		return fmt.Errorf("GGUF download failed size or SHA-256 verification (read %d of %d bytes)", n+4, s.Bytes)
	}
	return nil
}

// Ensure uses a verified cached artifact or downloads into a temporary file.
// The token is sent to huggingface.co only and never reaches Compose.
func Ensure(ctx context.Context, dir string, s domain.HFSource, token string, progress io.Writer) error {
	if err := Validate(s); err != nil {
		return err
	}
	cache := filepath.Join(dir, CacheDir)
	if err := os.Mkdir(cache, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if info, err := os.Lstat(cache); err != nil || !info.IsDir() {
		return fmt.Errorf("Hugging Face cache must be a real directory")
	}
	if len(s.Files) != 0 {
		for _, a := range s.Files {
			artifact := s
			artifact.File, artifact.Bytes = a.Name, a.Bytes
			if err := ensureFile(ctx, cache, a.Name, artifact, token, progress, func(r io.Reader, out io.Writer) error {
				return verifyArtifact(r, out, a)
			}); err != nil {
				return fmt.Errorf("%s: %w", a.Name, err)
			}
		}
		return nil
	}
	return ensureFile(ctx, cache, FileName, s, token, progress, func(r io.Reader, out io.Writer) error {
		return verifyGGUF(r, s, out)
	})
}

func ensureFile(ctx context.Context, cache, name string, s domain.HFSource, token string, progress io.Writer, verify func(io.Reader, io.Writer) error) error {
	path := filepath.Join(cache, name)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() != s.Bytes {
			return fmt.Errorf("cached model file differs from the plan; choose a new project directory")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return verify(&contextReader{ctx: ctx, r: f}, io.Discard)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	client := artifactClient()
	req, err := request(ctx, http.MethodGet, s, token)
	if err != nil {
		return fmt.Errorf("could not prepare Hugging Face download")
	}
	fmt.Fprintf(progress, "Downloading %s (%d MiB) from Hugging Face...\n", s.File, s.Bytes>>20)
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Hugging Face download interrupted or unavailable; rerun up")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return hubError(res.StatusCode)
	}
	tmp, err := os.CreateTemp(cache, ".download-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = verify(res.Body, &downloadProgress{dst: tmp, log: progress, size: s.Bytes})
	if ctx.Err() != nil {
		err = fmt.Errorf("download cancelled after timeout or user request: %w", ctx.Err())
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type downloadProgress struct {
	dst                     io.Writer
	log                     io.Writer
	size, written, reported int64
}

func (w *downloadProgress) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.written += int64(n)
	if w.written-w.reported >= 32<<20 {
		fmt.Fprintf(w.log, "Hugging Face: %d / %d MiB\n", w.written>>20, w.size>>20)
		w.reported = w.written
	}
	return n, err
}

func artifactClient() *http.Client {
	return &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("invalid artifact redirect")
		}
		if req.URL.Host != "huggingface.co" {
			req.Header.Del("Authorization")
			req.Header.Del("Referer")
		}
		return nil
	}}
}
