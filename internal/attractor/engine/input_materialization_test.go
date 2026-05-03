package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInputMaterialization_DefaultIncludeSeedsDoD(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".ai"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteInputFile(t, filepath.Join(source, ".ai", "definition_of_done.md"), "line-by-line acceptance criteria\n")

	manifest, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          nil,
		DefaultInclude:   []string{".ai/**"},
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest")
	}
	assertExists(t, filepath.Join(target, ".ai", "definition_of_done.md"))
}

func TestInputMaterialization_TransitiveReferenceClosureIncludesReferencedFiles(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	mustWriteInputFile(t, filepath.Join(source, ".ai", "definition_of_done.md"), "See [tests](../tests.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "tests.md"), "integration checks\n")

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{".ai/**"},
		DefaultInclude:   nil,
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	assertExists(t, filepath.Join(target, ".ai", "definition_of_done.md"))
	assertExists(t, filepath.Join(target, "tests.md"))
}

func TestInputMaterialization_DoesNotFollowArtifactGlobReferences(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	mustWriteInputFile(t, filepath.Join(source, "docs", "spec.md"), "Ignore pattern: `**/node_modules/**`\n")
	mustWriteInputFile(t, filepath.Join(source, "node_modules", "pkg", "README.md"), "dependency docs\n")

	manifest, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{"docs/spec.md"},
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	assertExists(t, filepath.Join(target, "docs", "spec.md"))
	assertNotExists(t, filepath.Join(target, "node_modules", "pkg", "README.md"))
	if len(manifest.ResolvedFiles) != 1 || manifest.ResolvedFiles[0] != "docs/spec.md" {
		t.Fatalf("resolved files = %v, want only docs/spec.md", manifest.ResolvedFiles)
	}
}

func TestInputMaterialization_RecursiveChainIncludesAllFiles(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	mustWriteInputFile(t, filepath.Join(source, "docs", "a.md"), "[b](b.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "docs", "b.md"), "[c](c.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "docs", "c.md"), "done\n")

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{"docs/a.md"},
		DefaultInclude:   nil,
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	assertExists(t, filepath.Join(target, "docs", "a.md"))
	assertExists(t, filepath.Join(target, "docs", "b.md"))
	assertExists(t, filepath.Join(target, "docs", "c.md"))
}

func TestInputMaterialization_DeepRecursionHasNoFixedDepthCap(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	const count = 1500
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("doc_%04d.md", i)
		content := "done\n"
		if i < count {
			next := fmt.Sprintf("doc_%04d.md", i+1)
			content = fmt.Sprintf("see [%s](%s)\n", next, next)
		}
		mustWriteInputFile(t, filepath.Join(source, "docs", name), content)
	}

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{"docs/doc_0001.md"},
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	assertExists(t, filepath.Join(target, "docs", "doc_1500.md"))
}

func TestInputMaterialization_IncludePatternWithoutMatchesFailsDeterministically(t *testing.T) {
	source := t.TempDir()
	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{"missing/**/*.md"},
		FollowReferences: true,
	})
	if err == nil {
		t.Fatal("expected include-missing error")
	}
	missingErr, ok := err.(*inputIncludeMissingError)
	if !ok {
		t.Fatalf("expected *inputIncludeMissingError, got %T (%v)", err, err)
	}
	if len(missingErr.Patterns) != 1 || missingErr.Patterns[0] != "missing/**/*.md" {
		t.Fatalf("unexpected missing patterns: %+v", missingErr.Patterns)
	}
	if !strings.Contains(err.Error(), "input_include_missing") {
		t.Fatalf("error should contain deterministic reason, got: %v", err)
	}
}

func TestInputMaterialization_DefaultIncludeWithoutMatchesDoesNotFail(t *testing.T) {
	source := t.TempDir()
	manifest, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          nil,
		DefaultInclude:   []string{"missing/**/*.md"},
		FollowReferences: true,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest")
	}
	if len(manifest.ResolvedFiles) != 0 {
		t.Fatalf("expected no resolved files, got: %+v", manifest.ResolvedFiles)
	}
}

func TestInputMaterialization_DefaultIncludeSkipsArtifactDocsForReferenceTraversal(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	mustWriteInputFile(t, filepath.Join(source, ".ai", "definition_of_done.md"), "See [tests](../tests.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "tests.md"), "integration checks\n")
	mustWriteInputFile(t, filepath.Join(source, ".ai", "benchmarks", "run1", "logs", "prompt.md"), "See [noise](../../../../docs/noise.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "docs", "noise.md"), "artifact noise\n")

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		DefaultInclude:   []string{".ai/**"},
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}

	assertExists(t, filepath.Join(target, ".ai", "definition_of_done.md"))
	assertExists(t, filepath.Join(target, "tests.md"))
	if _, statErr := os.Stat(filepath.Join(target, "docs", "noise.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected artifact-only reference to be skipped, stat err=%v", statErr)
	}
}

func TestInputMaterialization_ExplicitIncludeTraversesArtifactDocs(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	mustWriteInputFile(t, filepath.Join(source, ".ai", "benchmarks", "run1", "logs", "prompt.md"), "See [required](../../../../docs/required.md)\n")
	mustWriteInputFile(t, filepath.Join(source, "docs", "required.md"), "must materialize\n")

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{".ai/benchmarks/**/prompt.md"},
		FollowReferences: true,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}

	assertExists(t, filepath.Join(target, "docs", "required.md"))
}

// TestCopyInputFile_SelfCopyPreservesContent verifies that copyInputFile does not
// truncate a file when source and target resolve to the same inode (the
// "worktree-as-both-source-and-target" case in materializeStageInputs).
func TestCopyInputFile_SelfCopyPreservesContent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "spec.md")
	content := "important content\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyInputFile(file, file); err != nil {
		t.Fatalf("copyInputFile self-copy returned error: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("self-copy truncated file: got %d bytes, want %d", len(got), len(content))
	}
}

// TestInputMaterialization_SelfCopyPreservesWorktreeFiles verifies that when
// sourceRoots and targetRoot both point to the same directory (as in
// materializeStageInputs), files already in the worktree are not truncated.
func TestInputMaterialization_SelfCopyPreservesWorktreeFiles(t *testing.T) {
	worktree := t.TempDir()
	content := "design doc content\n"
	mustWriteInputFile(t, filepath.Join(worktree, ".ai", "spec.md"), content)

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{worktree},
		Include:          []string{".ai/**"},
		FollowReferences: false,
		TargetRoot:       worktree, // same as source root — the self-copy scenario
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(worktree, ".ai", "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("input materialization truncated .ai/spec.md: got %d bytes, want %d", len(got), len(content))
	}
}

// TestInputMaterialization_GitDirSkippedWhenCopyingToWorktree verifies that
// files under .git/ in the source repo are never materialized into the worktree
// target. A git worktree has .git as a plain *file* (the worktree pointer), so
// any attempt to MkdirAll({target}/.git) would fail with "not a directory".
func TestInputMaterialization_GitDirSkippedWhenCopyingToWorktree(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	// Simulate a git repo: .git/ is a directory with real files inside.
	mustWriteInputFile(t, filepath.Join(source, ".git", "config"), "[core]\n\trepositoryformatversion = 0\n")
	mustWriteInputFile(t, filepath.Join(source, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWriteInputFile(t, filepath.Join(source, "spec.md"), "project spec\n")

	// Simulate a worktree target: .git is a plain file (the worktree pointer).
	if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: /some/repo/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := materializeInputClosure(context.Background(), InputMaterializationOptions{
		SourceRoots:      []string{source},
		Include:          []string{"**"},
		FollowReferences: false,
		TargetRoot:       target,
	})
	if err != nil {
		t.Fatalf("materializeInputClosure: %v", err)
	}

	// spec.md should be copied; .git/* should be skipped entirely.
	assertExists(t, filepath.Join(target, "spec.md"))
	// .git must remain a plain file, not become a directory.
	info, err := os.Lstat(filepath.Join(target, ".git"))
	if err != nil {
		t.Fatalf("stat target/.git: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("target/.git should remain a regular file, got mode %v", info.Mode())
	}
}

func mustWriteInputFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist, stat err=%v", path, err)
	}
}
