package artifactstore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRegisteredUploadIsStable(t *testing.T) {
	store, err := NewLocal(privateArtifactDir(t))
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	uploadURI, err := store.UploadURI("actor:operation")
	if err != nil {
		t.Fatalf("upload URI: %v", err)
	}
	first, err := store.Put(context.Background(), uploadURI, strings.NewReader("artifact content"))
	if err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	second, err := store.Put(context.Background(), uploadURI, strings.NewReader("artifact content"))
	if err != nil {
		t.Fatalf("put duplicate artifact: %v", err)
	}
	if first.URI != second.URI || first.Digest != second.Digest || first.Size != int64(len("artifact content")) {
		t.Fatalf("registered upload mismatch: %#v %#v", first, second)
	}
	reader, err := store.Open(context.Background(), first.URI)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "artifact content" {
		t.Fatalf("read artifact: %v %q", err, content)
	}
}

func TestLocalWritesRegisteredUploadURI(t *testing.T) {
	store, err := NewLocal(privateArtifactDir(t))
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	uploadURI, err := store.UploadURI("actor:operation")
	if err != nil {
		t.Fatalf("upload URI: %v", err)
	}
	blob, err := store.Put(context.Background(), uploadURI, strings.NewReader("registered content"))
	if err != nil {
		t.Fatalf("put registered artifact: %v", err)
	}
	if blob.URI != uploadURI {
		t.Fatalf("blob URI = %q, want registered URI %q", blob.URI, uploadURI)
	}
	reader, err := store.Open(context.Background(), blob.URI)
	if err != nil {
		t.Fatalf("open registered artifact: %v", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "registered content" {
		t.Fatalf("registered content: %v %q", err, content)
	}
}

func TestLocalDeleteIsIdempotent(t *testing.T) {
	store, err := NewLocal(privateArtifactDir(t))
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	uploadURI, err := store.UploadURI("delete-operation")
	if err != nil {
		t.Fatalf("upload URI: %v", err)
	}
	blob, err := store.Put(context.Background(), uploadURI, strings.NewReader("temporary content"))
	if err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	if err := store.Delete(context.Background(), blob.URI); err != nil {
		t.Fatalf("delete artifact: %v", err)
	}
	if _, err := store.Open(context.Background(), blob.URI); err == nil {
		t.Fatal("expected deleted Artifact Blob to be unavailable")
	}
	if err := store.Delete(context.Background(), blob.URI); err != nil {
		t.Fatalf("delete missing artifact: %v", err)
	}
}

func TestLocalRejectsForeignURI(t *testing.T) {
	store, err := NewLocal(privateArtifactDir(t))
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	if _, err := store.Open(context.Background(), "kairos://blobs/uploads/../../secret"); err == nil {
		t.Fatal("expected invalid URI to be rejected")
	}
}

func TestLocalRejectsExistingFilesystemPermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}
	if _, err := NewLocal(root); err == nil {
		t.Fatal("expected an existing group-readable artifact directory to be rejected")
	}
	assertPermissions(t, root, 0o755)
}

func TestLocalAcceptsPrivateExistingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}
	if _, err := NewLocal(root); err != nil {
		t.Fatalf("new local store: %v", err)
	}
	assertPermissions(t, root, 0o700)
}

func TestLocalRejectsUnsafeRoots(t *testing.T) {
	if _, err := NewLocal(string(filepath.Separator)); err == nil {
		t.Fatal("expected filesystem root to be rejected")
	}
	file := filepath.Join(t.TempDir(), "artifact-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create artifact file: %v", err)
	}
	if _, err := NewLocal(file); err == nil {
		t.Fatal("expected artifact file path to be rejected")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create symlink target: %v", err)
	}
	link := filepath.Join(t.TempDir(), "artifact-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create artifact symlink: %v", err)
	}
	if _, err := NewLocal(link); err == nil {
		t.Fatal("expected artifact symlink to be rejected")
	}
}

func TestLocalReplacesArtifactWithPrivatePermissions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create artifact root: %v", err)
	}
	store, err := NewLocal(root)
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}

	uploadURI, err := store.UploadURI("permission-operation")
	if err != nil {
		t.Fatalf("upload URI: %v", err)
	}
	if _, err := store.Put(context.Background(), uploadURI, strings.NewReader("first")); err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	target, err := store.resolve(uploadURI)
	if err != nil {
		t.Fatalf("resolve artifact: %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("widen artifact permissions: %v", err)
	}
	if err := os.Chmod(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("widen artifact directory permissions: %v", err)
	}
	if _, err := store.Put(context.Background(), uploadURI, strings.NewReader("second")); err != nil {
		t.Fatalf("replace artifact: %v", err)
	}

	assertPermissions(t, target, 0o600)
	for directory := filepath.Dir(target); ; directory = filepath.Dir(directory) {
		assertPermissions(t, directory, 0o700)
		if directory == store.root {
			break
		}
	}
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %q = %04o, want %04o", path, got, want)
	}
}

func privateArtifactDir(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create private artifact root: %v", err)
	}
	return root
}
