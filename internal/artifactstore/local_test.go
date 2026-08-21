package artifactstore

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalContentAddressing(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	first, err := store.Put(context.Background(), LocalStoreURI, strings.NewReader("artifact content"))
	if err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	second, err := store.Put(context.Background(), LocalStoreURI, strings.NewReader("artifact content"))
	if err != nil {
		t.Fatalf("put duplicate artifact: %v", err)
	}
	if first.URI != second.URI || first.Digest != second.Digest || first.Size != int64(len("artifact content")) {
		t.Fatalf("content addressing mismatch: %#v %#v", first, second)
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

func TestLocalRejectsForeignURI(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new local store: %v", err)
	}
	if _, err := store.Open(context.Background(), "kairos://blobs/sha256/../../secret"); err == nil {
		t.Fatal("expected invalid URI to be rejected")
	}
}
