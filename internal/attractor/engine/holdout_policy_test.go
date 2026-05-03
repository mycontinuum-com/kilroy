package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danshapiro/kilroy/internal/attractor/dot"
	"github.com/danshapiro/kilroy/internal/attractor/model"
	"github.com/danshapiro/kilroy/internal/attractor/runtime"
)

type holdoutProbeHandler struct {
	t             *testing.T
	path          string
	wantVisible   bool
	writeLeak     bool
	observedState string
}

func (h *holdoutProbeHandler) Execute(_ context.Context, exec *Execution, node *model.Node) (runtime.Outcome, error) {
	_, err := os.Stat(filepath.Join(exec.WorktreeDir, h.path))
	visible := err == nil
	if visible {
		h.observedState = "visible"
	} else if os.IsNotExist(err) {
		h.observedState = "hidden"
	} else {
		h.t.Fatalf("stat holdout path: %v", err)
	}
	if visible != h.wantVisible {
		h.t.Fatalf("holdout visible=%v, want %v", visible, h.wantVisible)
	}
	if h.writeLeak {
		leakPath := filepath.Join(exec.LogsRoot, node.ID, "implementation_log.md")
		if err := os.WriteFile(leakPath, []byte("opened products/serenity/cli/SCENARIOS.md\n"), 0o644); err != nil {
			h.t.Fatalf("write leak artifact: %v", err)
		}
	}
	return runtime.Outcome{Status: runtime.StatusSuccess}, nil
}

func TestResolveHoldoutPolicyRejectsUntrackedPath(t *testing.T) {
	repo := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "SCENARIOS.md"), []byte("holdout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := graphWithHoldoutNodes(t)
	cfg := runConfigWithHoldout(repo, "SCENARIOS.md")

	_, err := ResolveHoldoutPolicy(cfg, g, repo)
	if err == nil {
		t.Fatal("expected untracked holdout path to be rejected")
	}
	if !strings.Contains(err.Error(), "tracked") {
		t.Fatalf("expected tracked-path error, got: %v", err)
	}
}

func TestEngineExecuteNodeHidesAndRestoresHoldoutForHiddenNode(t *testing.T) {
	repo := initTestRepo(t)
	writeAndCommitHoldout(t, repo, "SCENARIOS.md", "secret scenario\n")
	g := graphWithHoldoutNodes(t)
	cfg := runConfigWithHoldout(repo, "SCENARIOS.md")
	handler := &holdoutProbeHandler{t: t, path: "SCENARIOS.md", wantVisible: false}
	eng := newHoldoutTestEngine(t, g, repo, cfg, handler)

	out, err := executeHoldoutNode(t, eng, g.Nodes["implementation"])
	if err != nil {
		t.Fatalf("executeNode: %v", err)
	}
	if out.Status != runtime.StatusSuccess {
		t.Fatalf("status = %q, want success", out.Status)
	}
	if handler.observedState != "hidden" {
		t.Fatalf("handler observed %q, want hidden", handler.observedState)
	}
	if got := readHoldoutFile(t, filepath.Join(repo, "SCENARIOS.md")); got != "secret scenario\n" {
		t.Fatalf("restored content = %q", got)
	}
	if out := runCmdOut(t, repo, "git", "ls-files", "-v", "--", "SCENARIOS.md"); strings.HasPrefix(out, "S") {
		t.Fatalf("skip-worktree flag still set: %q", out)
	}
}

func TestEngineExecuteNodeLeavesHoldoutVisibleForVisibleNode(t *testing.T) {
	repo := initTestRepo(t)
	writeAndCommitHoldout(t, repo, "SCENARIOS.md", "secret scenario\n")
	g := graphWithHoldoutNodes(t)
	cfg := runConfigWithHoldout(repo, "SCENARIOS.md")
	handler := &holdoutProbeHandler{t: t, path: "SCENARIOS.md", wantVisible: true}
	eng := newHoldoutTestEngine(t, g, repo, cfg, handler)

	out, err := executeHoldoutNode(t, eng, g.Nodes["review"])
	if err != nil {
		t.Fatalf("executeNode: %v", err)
	}
	if out.Status != runtime.StatusSuccess {
		t.Fatalf("status = %q, want success", out.Status)
	}
	if handler.observedState != "visible" {
		t.Fatalf("handler observed %q, want visible", handler.observedState)
	}
}

func TestEngineExecuteNodeFailsOnHoldoutContaminationAndRestoresFile(t *testing.T) {
	repo := initTestRepo(t)
	writeAndCommitHoldout(t, repo, "SCENARIOS.md", "secret scenario\n")
	g := graphWithHoldoutNodes(t)
	cfg := runConfigWithHoldout(repo, "SCENARIOS.md")
	handler := &holdoutProbeHandler{t: t, path: "SCENARIOS.md", wantVisible: false, writeLeak: true}
	eng := newHoldoutTestEngine(t, g, repo, cfg, handler)

	out, err := executeHoldoutNode(t, eng, g.Nodes["implementation"])
	if err != nil {
		t.Fatalf("executeNode: %v", err)
	}
	if out.Status != runtime.StatusFail {
		t.Fatalf("status = %q, want fail", out.Status)
	}
	if !strings.Contains(out.FailureReason, "holdout_contamination") {
		t.Fatalf("failure_reason = %q", out.FailureReason)
	}
	if got := readHoldoutFile(t, filepath.Join(repo, "SCENARIOS.md")); got != "secret scenario\n" {
		t.Fatalf("restored content = %q", got)
	}
	report := filepath.Join(eng.LogsRoot, "implementation", "holdout_report.json")
	if _, err := os.Stat(report); err != nil {
		t.Fatalf("expected holdout report: %v", err)
	}
}

func graphWithHoldoutNodes(t *testing.T) *model.Graph {
	t.Helper()
	g, err := dot.Parse([]byte(`
digraph G {
  implementation [shape=box, type="holdout_probe", class="implementation"]
  review [shape=box, type="holdout_probe", class="review"]
}
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return g
}

func runConfigWithHoldout(repo, rel string) *RunConfigFile {
	cfg := &RunConfigFile{Version: 1}
	cfg.Repo.Path = repo
	cfg.Visibility.Holdouts = map[string]HoldoutConfig{
		"scenarios": {
			Paths: []string{rel},
			Visible: HoldoutVisibilityConfig{
				Classes: []string{"review"},
			},
			Scan: HoldoutScanConfig{
				DenyPatterns: []string{`SCENARIOS\.md`, `products/serenity/cli/scenarios`},
			},
		},
	}
	return cfg
}

func newHoldoutTestEngine(t *testing.T, g *model.Graph, repo string, cfg *RunConfigFile, handler *holdoutProbeHandler) *Engine {
	t.Helper()
	reg := NewDefaultRegistry()
	reg.Register("holdout_probe", handler)
	return &Engine{
		Graph:       g,
		Options:     RunOptions{RepoPath: repo, RunID: "holdout-test"},
		RunConfig:   cfg,
		LogsRoot:    t.TempDir(),
		WorktreeDir: repo,
		Context:     runtime.NewContext(),
		Registry:    reg,
	}
}

func executeHoldoutNode(t *testing.T, eng *Engine, node *model.Node) (runtime.Outcome, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return eng.executeNode(ctx, node)
}

func writeAndCommitHoldout(t *testing.T, repo string, rel string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runCmd(t, repo, "git", "add", rel)
	runCmd(t, repo, "git", "commit", "-m", "add holdout")
}

func readHoldoutFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
