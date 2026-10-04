// Package blob stores the bytes of artifact files.
//
// Blobs are content-addressed: the key is the hex sha256 of the bytes. Three
// properties follow that the rest of the system takes for granted:
//
//   - writing is idempotent, so an interrupted deploy can simply be retried;
//   - the same file in ten versions takes space once;
//   - the CLI can ask "which of these hashes are you missing?" and upload only
//     those, which is what makes redeploying a large bundle where only
//     index.html changed instant.
//
// Two implementations. The filesystem is the default: following the README
// gives you a Docker volume and no extra service to babysit. S3 exists for
// those who already run MinIO or a bucket and want a stateless backend.
package blob

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("blob not found")

type Store interface {
	// Put writes the bytes under the sha256 key. It is idempotent: if the blob
	// already exists nothing is rewritten.
	Put(ctx context.Context, sha256 string, r io.Reader, size int64) error
	// Get returns a ReadCloser over the blob's bytes.
	Get(ctx context.Context, sha256 string) (io.ReadCloser, error)
	// Has reports whether the blob is already present.
	Has(ctx context.Context, sha256 string) (bool, error)
	// Delete removes a blob. Only the garbage collector uses it.
	Delete(ctx context.Context, sha256 string) error
}
