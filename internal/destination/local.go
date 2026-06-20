package destination

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// localDest stores backups on the local (or mounted) filesystem.
type localDest struct {
	name string
	base string
}

func newLocal(cfg config.Destination) (Destination, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("local destination requires `path`")
	}
	return &localDest{name: cfg.Name, base: cfg.Path}, nil
}

func (d *localDest) Name() string   { return d.name }
func (d *localDest) Type() string   { return config.TypeLocal }
func (d *localDest) Readable() bool { return true }

func (d *localDest) path(key string) string {
	return filepath.Join(d.base, filepath.FromSlash(key))
}

func (d *localDest) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	dst := d.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Atomic publish so a crashed Put never leaves a half-written artifact.
	return os.Rename(tmp, dst)
}

func (d *localDest) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(d.path(key))
}

func (d *localDest) List(_ context.Context, prefix string) ([]Artifact, error) {
	root := d.base
	var out []Artifact
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			out = append(out, Artifact{Key: key, Size: info.Size(), ModTime: info.ModTime()})
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

func (d *localDest) Delete(_ context.Context, key string) error {
	err := os.Remove(d.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Test verifies the base directory is writable.
func (d *localDest) Test(_ context.Context) error {
	if err := os.MkdirAll(d.base, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", d.base, err)
	}
	probe := filepath.Join(d.base, ".pgproof-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("write to %q: %w", d.base, err)
	}
	return os.Remove(probe)
}
