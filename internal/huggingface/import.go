package huggingface

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"llmlauncher/internal/domain"
)

// Import copies only the artifacts pinned by the reviewed plan. It never
// downloads, runs model code, or changes the caller's original files.
func Import(ctx context.Context, dir string, source domain.HFSource, local string) error {
	if err := Validate(source); err != nil {
		return err
	}
	cache := filepath.Join(dir, CacheDir)
	if err := os.Mkdir(cache, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if info, err := os.Lstat(cache); err != nil || !info.IsDir() {
		return fmt.Errorf("model cache must be a real directory")
	}
	if len(source.Files) == 0 {
		return importFile(ctx, local, filepath.Join(cache, FileName), source.Bytes, func(r io.Reader, out io.Writer) error {
			return verifyGGUF(r, source, out)
		})
	}
	if info, err := os.Stat(local); err != nil || !info.IsDir() {
		return fmt.Errorf("select the Hugging Face snapshot folder containing config, tokenizer and safetensors files")
	}
	for _, artifact := range source.Files {
		if err := importFile(ctx, filepath.Join(local, artifact.Name), filepath.Join(cache, artifact.Name), artifact.Bytes,
			func(r io.Reader, out io.Writer) error { return verifyArtifact(r, out, artifact) }); err != nil {
			return fmt.Errorf("%s: %w", artifact.Name, err)
		}
	}
	return nil
}

func importFile(ctx context.Context, local, target string, size int64, verify func(io.Reader, io.Writer) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() || info.Size() != size {
			return fmt.Errorf("existing cached weights differ from the plan; choose a new project folder")
		}
		file, err := os.Open(target)
		if err != nil {
			return err
		}
		defer file.Close()
		err = verify(&contextReader{ctx: ctx, r: file}, io.Discard)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	if info, err := os.Stat(local); err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return fmt.Errorf("selected weights differ from the plan; select the exact model, format and revision before importing")
	}
	file, err := os.Open(local)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return fmt.Errorf("selected weights differ from the plan; select the exact model, format and revision before importing")
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".import-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	err = verify(&contextReader{ctx: ctx, r: file}, temp)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("selected weights differ from the plan: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Link installs verified data without replacing a destination. Filesystems
	// without hard links use exclusive creation instead.
	if err := os.Link(temp.Name(), target); err == nil {
		return nil
	} else if os.IsExist(err) {
		return fmt.Errorf("cached weights appeared during import; retry to verify them")
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, &contextReader{ctx: ctx, r: temp})
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(target)
	}
	return err
}
