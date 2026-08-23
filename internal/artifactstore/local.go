// Package artifactstore provides managed Artifact content stores.
package artifactstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ScienJus/kairos/internal/domain"
)

const LocalScheme = "kairos"

// Local stores managed blobs below one local directory.
type Local struct {
	root string
}

// NewLocal creates a local kairos:// Artifact Store.
func NewLocal(root string) (*Local, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("artifact directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact directory: %w", err)
	}
	return &Local{root: abs}, nil
}

func (*Local) Scheme() string { return LocalScheme }

func (s *Local) UploadURI(key string) (string, error) {
	digest := sha256.Sum256([]byte(key))
	return "kairos://blobs/uploads/" + hex.EncodeToString(digest[:]), nil
}

// Put writes content to the URI that was registered before the upload.
func (s *Local) Put(ctx context.Context, storeURI string, source io.Reader) (domain.ArtifactBlob, error) {
	configured, err := url.Parse(storeURI)
	if err != nil || configured.Scheme != LocalScheme {
		return domain.ArtifactBlob{}, fmt.Errorf("invalid local Artifact Store URI %q", storeURI)
	}
	if configured.Host == "blobs" {
		target, err := s.resolve(storeURI)
		if err != nil {
			return domain.ArtifactBlob{}, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("create artifact blob directory: %w", err)
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("create artifact blob: %w", err)
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(file, hash), &contextReader{ctx: ctx, source: source})
		var syncErr error
		if copyErr == nil {
			syncErr = file.Sync()
		}
		closeErr := file.Close()
		if copyErr != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("write artifact blob: %w", copyErr)
		}
		if syncErr != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("sync artifact blob: %w", syncErr)
		}
		if closeErr != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("close artifact blob: %w", closeErr)
		}
		if err := syncDirectoryChain(s.root, filepath.Dir(target)); err != nil {
			return domain.ArtifactBlob{}, err
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		return domain.ArtifactBlob{URI: storeURI, Digest: "sha256:" + digest, Size: written}, nil
	}
	return domain.ArtifactBlob{}, fmt.Errorf("artifact upload URI must be registered before writing")
}

func syncDirectoryChain(root, leaf string) error {
	for directory := leaf; ; directory = filepath.Dir(directory) {
		handle, err := os.Open(directory)
		if err != nil {
			return fmt.Errorf("open artifact directory for sync: %w", err)
		}
		syncErr := handle.Sync()
		closeErr := handle.Close()
		if syncErr != nil {
			return fmt.Errorf("sync artifact directory: %w", syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close artifact directory after sync: %w", closeErr)
		}
		if directory == root {
			return nil
		}
		if parent := filepath.Dir(directory); parent == directory {
			return fmt.Errorf("artifact directory %q is outside root %q", leaf, root)
		}
	}
}

// Open resolves a kairos:// URI without accepting caller-controlled filesystem paths.
func (s *Local) Open(_ context.Context, rawURI string) (io.ReadCloser, error) {
	path, err := s.resolve(rawURI)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact blob: %w", err)
	}
	return file, nil
}

// Delete removes managed content. Missing content is already collected.
func (s *Local) Delete(_ context.Context, rawURI string) error {
	path, err := s.resolve(rawURI)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete artifact blob: %w", err)
	}
	return nil
}

func (s *Local) resolve(rawURI string) (string, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme != LocalScheme || parsed.Host != "blobs" {
		return "", fmt.Errorf("invalid kairos artifact URI %q", rawURI)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "uploads" || len(parts[1]) != sha256.Size*2 {
		return "", fmt.Errorf("invalid kairos artifact URI %q", rawURI)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", fmt.Errorf("invalid kairos artifact digest: %w", err)
	}
	return filepath.Join(s.root, parts[0], parts[1][:2], parts[1][2:4], parts[1]), nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.source.Read(buffer)
	}
}
