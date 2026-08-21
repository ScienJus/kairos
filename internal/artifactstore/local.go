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
const LocalStoreURI = "kairos://"

// Local stores content-addressed blobs below one local directory.
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
	if err := os.MkdirAll(filepath.Join(abs, "staging"), 0o750); err != nil {
		return nil, fmt.Errorf("create artifact staging directory: %w", err)
	}
	return &Local{root: abs}, nil
}

func (*Local) Scheme() string { return LocalScheme }

// Put writes content once and returns its content-addressed URI.
func (s *Local) Put(ctx context.Context, storeURI string, source io.Reader) (domain.ArtifactBlob, error) {
	configured, err := url.Parse(storeURI)
	if err != nil || configured.Scheme != LocalScheme {
		return domain.ArtifactBlob{}, fmt.Errorf("invalid local Artifact Store URI %q", storeURI)
	}
	temporary, err := os.CreateTemp(filepath.Join(s.root, "staging"), "upload-*")
	if err != nil {
		return domain.ArtifactBlob{}, fmt.Errorf("create artifact upload: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), &contextReader{ctx: ctx, source: source})
	closeErr := temporary.Close()
	if copyErr != nil {
		return domain.ArtifactBlob{}, fmt.Errorf("write artifact upload: %w", copyErr)
	}
	if closeErr != nil {
		return domain.ArtifactBlob{}, fmt.Errorf("close artifact upload: %w", closeErr)
	}

	digest := hex.EncodeToString(hash.Sum(nil))
	target := filepath.Join(s.root, "sha256", digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return domain.ArtifactBlob{}, fmt.Errorf("create artifact blob directory: %w", err)
	}
	if _, err := os.Stat(target); err != nil {
		if !os.IsNotExist(err) {
			return domain.ArtifactBlob{}, fmt.Errorf("inspect artifact blob: %w", err)
		}
		if err := os.Rename(temporaryPath, target); err != nil {
			return domain.ArtifactBlob{}, fmt.Errorf("commit artifact blob: %w", err)
		}
	}
	return domain.ArtifactBlob{
		URI: "kairos://blobs/sha256/" + digest, Digest: "sha256:" + digest, Size: written,
	}, nil
}

// Open resolves a kairos:// URI without accepting caller-controlled filesystem paths.
func (s *Local) Open(_ context.Context, rawURI string) (io.ReadCloser, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme != LocalScheme || parsed.Host != "blobs" {
		return nil, fmt.Errorf("invalid kairos artifact URI %q", rawURI)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "sha256" || len(parts[1]) != sha256.Size*2 {
		return nil, fmt.Errorf("invalid kairos artifact URI %q", rawURI)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return nil, fmt.Errorf("invalid kairos artifact digest: %w", err)
	}
	path := filepath.Join(s.root, "sha256", parts[1][:2], parts[1][2:4], parts[1])
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact blob: %w", err)
	}
	return file, nil
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
