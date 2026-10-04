package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FS stores blobs on disk, sharded on the first two characters of the hash.
//
// Sharding is not vanity: without it, a directory with a hundred thousand
// files gets slow to list on ext4 and painful on a network volume.
type FS struct{ root string }

func NewFS(root string) (*FS, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("blob root: %w", err)
	}
	return &FS{root: root}, nil
}

func (f *FS) path(sha string) string {
	return filepath.Join(f.root, sha[:2], sha)
}

func (f *FS) Put(ctx context.Context, sha string, r io.Reader, _ int64) error {
	dst := f.path(sha)
	if _, err := os.Stat(dst); err == nil {
		// Same hash, same bytes: nothing to rewrite.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	// Write to a temporary file and rename: an interrupted Put never leaves a
	// truncated blob behind that would then be served as if it were valid.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func (f *FS) Get(ctx context.Context, sha string) (io.ReadCloser, error) {
	fh, err := os.Open(f.path(sha))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return fh, err
}

func (f *FS) Has(ctx context.Context, sha string) (bool, error) {
	_, err := os.Stat(f.path(sha))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (f *FS) Delete(ctx context.Context, sha string) error {
	err := os.Remove(f.path(sha))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
