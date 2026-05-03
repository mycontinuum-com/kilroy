package gitutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ListIgnoredFiles returns repo-relative paths of all gitignored files under
// dir. It runs: git ls-files --others --ignored --exclude-standard
func ListIgnoredFiles(dir string) ([]string, error) {
	stdout, _, err := runGit(dir, "ls-files", "--others", "--ignored", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(stdout, "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// CopyIgnoredFiles copies all gitignored files from srcDir into dstDir,
// preserving relative paths and file permissions. Intermediate directories are
// created as needed. Symlinks and non-regular files are skipped. Individual
// file errors are silently skipped; the function returns an error only if the
// ignored-file listing itself fails.
//
// Optional excludePrefixes (forward-slash form, e.g. ".ai/runs/") are matched
// against each repo-relative path; any matching file is skipped. This allows
// callers to exclude paths managed by other systems (e.g. the run-scoped
// lineage system owns .ai/runs/ and must not be overwritten by a raw copy).
func CopyIgnoredFiles(srcDir, dstDir string, excludePrefixes ...string) error {
	files, err := ListIgnoredFiles(srcDir)
	if err != nil {
		return err
	}
	for _, rel := range files {
		relFwd := filepath.ToSlash(rel)
		if shouldSkipIgnoredFileCopy(relFwd, excludePrefixes) {
			continue
		}
		src := filepath.Join(srcDir, rel)
		dst := filepath.Join(dstDir, rel)
		info, err := os.Lstat(src)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}
		_ = copyFileMode(src, dst, info.Mode())
	}
	return nil
}

func shouldSkipIgnoredFileCopy(rel string, excludePrefixes []string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	for _, pfx := range excludePrefixes {
		if strings.HasPrefix(rel, filepath.ToSlash(strings.TrimSpace(pfx))) {
			return true
		}
	}
	for _, segment := range strings.Split(rel, "/") {
		if strings.HasPrefix(segment, ".cargo-target") {
			return true
		}
		switch segment {
		case ".git", ".jj", ".pnpm-store", ".pytest_cache", ".turbo", ".venv", ".wasm-pack", "__pycache__", "build", "coverage", "dist", "logs", "managed", "node_modules", "target", "venv":
			return true
		}
	}
	if strings.HasPrefix(rel, ".ai/runs/") {
		return true
	}
	return false
}

func copyFileMode(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
