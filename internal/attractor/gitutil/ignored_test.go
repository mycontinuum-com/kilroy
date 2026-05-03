package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyIgnoredFilesSkipsArtifactDirectories(t *testing.T) {
	src := initTestRepo(t)
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte(".env\nnode_modules/\ndist/\n.ai/runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, src, "add gitignore")

	writeFile(t, filepath.Join(src, ".env"), "secret")
	writeFile(t, filepath.Join(src, "node_modules", "pkg", "index.js"), "dependency")
	writeFile(t, filepath.Join(src, "dist", "bundle.js"), "build")
	writeFile(t, filepath.Join(src, ".ai", "runs", "run-1", "artifact.txt"), "artifact")

	if err := CopyIgnoredFiles(src, dst); err != nil {
		t.Fatalf("CopyIgnoredFiles: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".env")); err != nil {
		t.Fatalf("expected .env to be copied: %v", err)
	}
	for _, rel := range []string{
		filepath.Join("node_modules", "pkg", "index.js"),
		filepath.Join("dist", "bundle.js"),
		filepath.Join(".ai", "runs", "run-1", "artifact.txt"),
	} {
		if _, err := os.Stat(filepath.Join(dst, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected artifact %s not to be copied, stat err=%v", rel, err)
		}
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
