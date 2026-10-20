package provision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImportRejectsUnsafeSource(t *testing.T) {
	for _, source := range []Source{{Driver: "postgres", Prefix: "x;DROP TABLE users", TexturesDir: t.TempDir()}, {Driver: "sqlite", TexturesDir: t.TempDir()}, {Driver: "mysql", TexturesDir: "relative"}} {
		_, err := (&Store{TexturesDir: t.TempDir()}).Run(context.Background(), source, true, "", "", func(string, int) {})
		if err == nil {
			t.Fatal("unsafe source accepted")
		}
	}
}
func TestImportTextureIntegrity(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(buf.Bytes()))
	path := filepath.Join(dir, hash)
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	data, w, h, err := sourceFile(dir, hash)
	if err != nil || w != 64 || h != 64 || !bytes.Equal(data, buf.Bytes()) {
		t.Fatal("valid PNG must preserve original bytes")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := sourceFile(dir, hash); err == nil {
		t.Fatal("corrupt hash accepted")
	}
	if _, _, _, err := sourceFile(dir, "../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
