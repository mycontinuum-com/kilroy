package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danshapiro/kilroy/internal/attractor/runtime"
)

func TestRunWithConfig_GitWorkspaceDoesNotCopyIgnoredFilesAndUsesSetupCommands(t *testing.T) {
	repo := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("node_modules/\ndist/\nlocal.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, repo, "git", "add", ".gitignore")
	runCmd(t, repo, "git", "commit", "-m", "ignore generated files")
	if err := os.WriteFile(filepath.Join(repo, "local.env"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "node_modules", "pkg", "index.js"), []byte("warm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dist", "bundle.js"), []byte("built\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := minimalToolRunConfig(t, repo)
	cfg.Setup.Commands = []string{
		"test ! -e local.env && test ! -e node_modules/pkg/index.js && test ! -e dist/bundle.js && mkdir -p node_modules/pkg dist && echo hydrated > node_modules/pkg/index.js && echo built > dist/bundle.js && echo setup > setup-marker.txt",
	}
	cfg.Setup.TimeoutMS = 10000

	dot := []byte(`
digraph G {
  start [shape=Mdiamond]
  verify [shape=parallelogram, tool_command="grep -q hydrated node_modules/pkg/index.js && grep -q built dist/bundle.js && grep -q setup setup-marker.txt"]
  exit [shape=Msquare]
  start -> verify
  verify -> exit [condition="outcome=success"]
}
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := RunWithConfig(ctx, dot, cfg, RunOptions{RunID: "test-no-ignored-copy", LogsRoot: t.TempDir(), DisableCXDB: true})
	if err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if res.FinalStatus != runtime.FinalSuccess {
		t.Fatalf("final status: got %q want %q", res.FinalStatus, runtime.FinalSuccess)
	}
}

func TestRunWithConfig_ParallelBranchWorktreesRunSetupCommands(t *testing.T) {
	repo := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("setup-cwd.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, repo, "git", "add", ".gitignore")
	runCmd(t, repo, "git", "commit", "-m", "ignore setup marker")

	cfg := minimalToolRunConfig(t, repo)
	cfg.Setup.Commands = []string{"pwd > setup-cwd.txt"}
	cfg.Setup.TimeoutMS = 10000

	dot := []byte(`
digraph P {
  graph [goal="parallel setup test"]
  start [shape=Mdiamond]
  par [shape=component]
  a [shape=parallelogram, tool_command="test -s setup-cwd.txt && echo a > a.txt"]
  b [shape=parallelogram, tool_command="test -s setup-cwd.txt && echo b > b.txt"]
  join [shape=tripleoctagon]
  exit [shape=Msquare]

  start -> par
  par -> a
  par -> b
  a -> join
  b -> join
  join -> exit [condition="outcome=success"]
}
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	logsRoot := t.TempDir()
	res, err := RunWithConfig(ctx, dot, cfg, RunOptions{RunID: "test-branch-setup", LogsRoot: logsRoot, DisableCXDB: true})
	if err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if res.FinalStatus != runtime.FinalSuccess {
		t.Fatalf("final status: got %q want %q", res.FinalStatus, runtime.FinalSuccess)
	}
}

func TestRunWithConfig_CodexStructuredOutputCanProvideStageStatus(t *testing.T) {
	repo := initTestRepo(t)
	pinned := writePinnedCatalog(t)
	logsRoot := t.TempDir()

	cli := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(cli, []byte(`#!/usr/bin/env bash
set -euo pipefail

out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      out="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [[ -n "$out" ]]; then
  cat > "$out" <<'JSON'
{"final":"needs repair","summary":"route to repair","status":"validation_repair","failure_reason":"validation script missing","failure_class":"deterministic"}
JSON
fi

echo '{"type":"start"}'
echo '{"type":"done","text":"needs repair"}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	seedHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(seedHome, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedHome, ".codex", "auth.json"), []byte(`{"token":"seeded"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", seedHome)

	cfg := &RunConfigFile{Version: 1}
	cfg.Repo.Path = repo
	cfg.CXDB.BinaryAddr = "127.0.0.1:9009"
	cfg.CXDB.HTTPBaseURL = "http://127.0.0.1:9010"
	cfg.LLM.CLIProfile = "test_shim"
	cfg.LLM.Providers = map[string]ProviderConfig{
		"openai": {Backend: BackendCLI, Executable: cli},
	}
	cfg.ModelDB.OpenRouterModelInfoPath = pinned
	cfg.ModelDB.OpenRouterModelInfoUpdatePolicy = "pinned"
	cfg.Git.RunBranchPrefix = "attractor/run"

	dot := []byte(`
digraph G {
  graph [goal="structured status"]
  start [shape=Mdiamond]
  router [shape=box, llm_provider=openai, llm_model=gpt-5.2, prompt="route"]
  repair [shape=parallelogram, tool_command="echo repaired > repaired.txt"]
  fallback [shape=parallelogram, tool_command="echo fallback > fallback.txt"]
  exit [shape=Msquare]

  start -> router
  router -> repair [condition="outcome=validation_repair"]
  router -> fallback
  repair -> exit [condition="outcome=success"]
  fallback -> exit [condition="outcome=success"]
}
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := RunWithConfig(ctx, dot, cfg, RunOptions{RunID: "test-structured-status", LogsRoot: logsRoot, AllowTestShim: true, DisableCXDB: true})
	if err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := strings.TrimSpace(runCmdOut(t, repo, "git", "show", res.FinalCommitSHA+":repaired.txt")); got != "repaired" {
		t.Fatalf("repaired.txt: got %q want %q", got, "repaired")
	}
	b, err := os.ReadFile(filepath.Join(res.LogsRoot, "router", "status.json"))
	if err != nil {
		t.Fatalf("read router/status.json: %v", err)
	}
	out, err := runtime.DecodeOutcomeJSON(b)
	if err != nil {
		t.Fatalf("decode router/status.json: %v", err)
	}
	if out.Status != runtime.StageStatus("validation_repair") {
		t.Fatalf("router status: got %q want validation_repair", out.Status)
	}
}

func minimalToolRunConfig(t *testing.T, repo string) *RunConfigFile {
	t.Helper()
	cfg := &RunConfigFile{Version: 1}
	cfg.Repo.Path = repo
	cfg.CXDB.BinaryAddr = "127.0.0.1:9009"
	cfg.CXDB.HTTPBaseURL = "http://127.0.0.1:9010"
	cfg.ModelDB.OpenRouterModelInfoPath = writePinnedCatalog(t)
	cfg.ModelDB.OpenRouterModelInfoUpdatePolicy = "pinned"
	cfg.Git.RunBranchPrefix = "attractor/run"
	return cfg
}
