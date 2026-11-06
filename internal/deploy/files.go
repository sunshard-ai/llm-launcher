package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"llmlauncher/internal/domain"
)

type manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Project       string `json:"project"`
	Owner         string `json:"owner"`
	PlanSHA256    string `json:"plan_sha256"`
	ComposeSHA256 string `json:"compose_sha256"`
	EnvSHA256     string `json:"env_sha256"`
}

type runState struct {
	SchemaVersion int                `json:"schema_version"`
	Owner         string             `json:"owner"`
	Project       string             `json:"project"`
	Status        string             `json:"status"`
	UpdatedAt     string             `json:"updated_at"`
	LastReadyAt   string             `json:"last_ready_at,omitempty"`
	ImageID       string             `json:"image_id,omitempty"`
	ImageDigests  []string           `json:"image_digests,omitempty"`
	ModelRef      string             `json:"model_ref"`
	ModelDigest   string             `json:"model_digest,omitempty"`
	ModelRevision string             `json:"model_revision,omitempty"`
	Smoke         domain.SmokeResult `json:"smoke"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func canonicalDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("project directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func acquireLock(dir string) (func(), error) {
	path := filepath.Join(dir, lockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another operation owns %s; if an earlier process crashed, verify no launcher operation is running before deleting this lock", path)
		}
		return nil, err
	}
	_, writeErr := fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, closeErr
	}
	return func() { _ = os.Remove(path) }, nil
}

func exclusiveFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// WritePlan creates a reviewable deployment bundle and refuses to overwrite files.
// It never calls Docker or downloads a model.
func WritePlan(dir string, p domain.Plan) error {
	compose, err := Render(p)
	if err != nil {
		return err
	}
	dir, err = canonicalDir(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	unlock, err := acquireLock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != lockFile {
			return fmt.Errorf("%s is not empty; choose a new project directory (existing plans are never overwritten)", dir)
		}
	}
	plan, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	plan = append(plan, '\n')
	m := manifest{SchemaVersion: 1, Project: p.Project, Owner: fingerprint(p), PlanSHA256: digest(plan), ComposeSHA256: digest(compose), EnvSHA256: digest([]byte(envContents))}
	manifestBytes, _ := json.MarshalIndent(m, "", "  ")
	manifestBytes = append(manifestBytes, '\n')
	files := []struct {
		name string
		data []byte
	}{{PlanFile, plan}, {ComposeFile, compose}, {envFile, []byte(envContents)}, {ManifestFile, manifestBytes}}
	var created []string
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := exclusiveFile(path, f.data); err != nil {
			for _, ownPath := range created {
				_ = os.Remove(ownPath)
			}
			return err
		}
		created = append(created, path)
	}
	return nil
}

func readSmall(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("managed file %s must be a regular file, not a symlink", path)
	}
	if info.Size() > 2<<20 {
		return nil, fmt.Errorf("managed file %s exceeds 2 MiB", path)
	}
	return os.ReadFile(path)
}

func strictJSON(b []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON data")
	}
	return nil
}

// ReadPlan checks the complete bundle and regenerates Compose to detect drift.
// Checksums detect accidental changes; they are not a digital signature.
func ReadPlan(dir string) (domain.Plan, error) {
	var p domain.Plan
	dir, err := canonicalDir(dir)
	if err != nil {
		return p, err
	}
	mb, err := readSmall(filepath.Join(dir, ManifestFile))
	if err != nil {
		return p, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := strictJSON(mb, &m); err != nil {
		return p, fmt.Errorf("invalid manifest: %w", err)
	}
	if m.SchemaVersion != 1 {
		return p, fmt.Errorf("unsupported manifest schema")
	}
	pb, err := readSmall(filepath.Join(dir, PlanFile))
	if err != nil {
		return p, err
	}
	cb, err := readSmall(filepath.Join(dir, ComposeFile))
	if err != nil {
		return p, err
	}
	eb, err := readSmall(filepath.Join(dir, envFile))
	if err != nil {
		return p, err
	}
	if digest(pb) != m.PlanSHA256 || digest(cb) != m.ComposeSHA256 || digest(eb) != m.EnvSHA256 {
		return p, fmt.Errorf("managed deployment files changed; create a new plan instead of editing plan.json/compose.yaml/launcher env")
	}
	if !bytes.Equal(eb, []byte(envContents)) {
		return p, fmt.Errorf("managed Compose environment file changed")
	}
	if err := strictJSON(pb, &p); err != nil {
		return p, fmt.Errorf("invalid plan: %w", err)
	}
	if err := Validate(p); err != nil {
		return p, err
	}
	if p.Project != m.Project || fingerprint(p) != m.Owner {
		return p, fmt.Errorf("manifest ownership does not match the plan")
	}
	expected, err := Render(p)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(expected, cb) {
		return p, fmt.Errorf("Compose content differs from the validated plan")
	}
	return p, nil
}

func loadState(dir string, p domain.Plan) (runState, error) {
	s := runState{SchemaVersion: 1, Owner: fingerprint(p), Project: p.Project, ModelRef: p.Model.Ref, ModelRevision: p.Model.Revision}
	b, err := readSmall(filepath.Join(dir, stateFile))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var saved runState
	if err := strictJSON(b, &saved); err != nil {
		return s, fmt.Errorf("invalid state.json: %w", err)
	}
	s = saved
	if s.SchemaVersion != 1 || s.Owner != fingerprint(p) || s.Project != p.Project || s.ModelRef != p.Model.Ref {
		return s, fmt.Errorf("state.json belongs to a different plan")
	}
	if s.Status != "ready" && s.Status != "stopped" {
		return s, fmt.Errorf("state.json has an invalid deployment status")
	}
	if s.Status == "ready" && (s.ImageID == "" || !s.Smoke.OK || s.LastReadyAt == "") {
		return s, fmt.Errorf("state.json claims readiness without successful inference evidence")
	}
	return s, nil
}

func saveState(dir string, s runState) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	path := filepath.Join(dir, stateFile)
	if _, err := os.Lstat(path); err == nil {
		if _, err := readSmall(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
