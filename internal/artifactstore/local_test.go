package artifactstore

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalRegisteredUploadIsStable(t *testing.T) {
	store, err := NewLocal(t.TempDir())
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
	store, err := NewLocal(t.TempDir())
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
	store, err := NewLocal(t.TempDir())
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
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	if _, err := store.Open(context.Background(), "kairos://blobs/uploads/../../secret"); err == nil {
		t.Fatal("expected invalid URI to be rejected")
	}
}
