package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRunConfigFile_YAMLAndJSON(t *testing.T) {
	dir := t.TempDir()

	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile(yaml): %v", err)
	}
	if cfg.Version != 1 || strings.TrimSpace(cfg.Repo.Path) == "" {
		t.Fatalf("cfg: %+v", cfg)
	}
	if cfg.LLM.Providers["openai"].Backend != BackendAPI {
		t.Fatalf("openai backend: %q", cfg.LLM.Providers["openai"].Backend)
	}

	js := filepath.Join(dir, "run.json")
	if err := os.WriteFile(js, []byte(`{
  "version": 1,
  "repo": {"path": "/tmp/repo"},
  "cxdb": {"binary_addr": "127.0.0.1:9009", "http_base_url": "http://127.0.0.1:9010"},
  "llm": {"providers": {"anthropic": {"backend": "cli"}}},
  "modeldb": {"openrouter_model_info_path": "/tmp/catalog.json"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadRunConfigFile(js)
	if err != nil {
		t.Fatalf("LoadRunConfigFile(json): %v", err)
	}
	if cfg2.LLM.Providers["anthropic"].Backend != BackendCLI {
		t.Fatalf("anthropic backend: %q", cfg2.LLM.Providers["anthropic"].Backend)
	}
}

func TestLoadRunConfigFile_RejectsUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
unknown_top_level: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatal("expected strict decode error for unknown top-level key")
	}
	if !strings.Contains(err.Error(), "unknown_top_level") {
		t.Fatalf("expected error to mention unknown_top_level, got: %v", err)
	}
}

func TestLoadRunConfigFile_RejectsUnknownNestedKey(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
      backnd: cli
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatal("expected strict decode error for unknown nested key")
	}
	if !strings.Contains(err.Error(), "backnd") {
		t.Fatalf("expected error to mention backnd, got: %v", err)
	}
}

func TestLoadRunConfigFile_RejectsUnknownJSONTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	js := filepath.Join(dir, "run.json")
	if err := os.WriteFile(js, []byte(`{
  "version": 1,
  "repo": {"path": "/tmp/repo"},
  "cxdb": {"binary_addr": "127.0.0.1:9009", "http_base_url": "http://127.0.0.1:9010"},
  "llm": {"providers": {"openai": {"backend": "api"}}},
  "modeldb": {"openrouter_model_info_path": "/tmp/catalog.json"},
  "unknown_top_level": true
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRunConfigFile(js)
	if err == nil {
		t.Fatal("expected strict decode error for unknown top-level JSON key")
	}
	if !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), "unknown_top_level") {
		t.Fatalf("expected json unknown field error, got: %v", err)
	}
}

func TestLoadRunConfigFile_VisibilityHoldouts(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
visibility:
  holdouts:
    scenarios:
      paths:
        - products/serenity/cli/SCENARIOS.md
      visible:
        nodes:
          - final_report
        classes:
          - review
      scan:
        worktree_globs:
          - .ai/runs/$KILROY_RUN_ID/**
        deny_patterns:
          - SCENARIOS\.md
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	holdout := cfg.Visibility.Holdouts["scenarios"]
	if len(holdout.Paths) != 1 || holdout.Paths[0] != "products/serenity/cli/SCENARIOS.md" {
		t.Fatalf("paths = %v", holdout.Paths)
	}
	if len(holdout.Visible.Nodes) != 1 || holdout.Visible.Nodes[0] != "final_report" {
		t.Fatalf("visible nodes = %v", holdout.Visible.Nodes)
	}
	if len(holdout.Visible.Classes) != 1 || holdout.Visible.Classes[0] != "review" {
		t.Fatalf("visible classes = %v", holdout.Visible.Classes)
	}
	if len(holdout.Scan.WorktreeGlobs) != 1 || holdout.Scan.WorktreeGlobs[0] != ".ai/runs/$KILROY_RUN_ID/**" {
		t.Fatalf("worktree globs = %v", holdout.Scan.WorktreeGlobs)
	}
	if len(holdout.Scan.DenyPatterns) != 1 || holdout.Scan.DenyPatterns[0] != `SCENARIOS\.md` {
		t.Fatalf("deny patterns = %v", holdout.Scan.DenyPatterns)
	}
}

func TestLoadRunConfigFile_AllowsGraphAndTaskMetadata(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
graph: demo/rogue/rogue.dot
task: implement feature x
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("expected graph/task metadata to be accepted, got: %v", err)
	}
	if got, want := cfg.Graph, "demo/rogue/rogue.dot"; got != want {
		t.Fatalf("graph=%q want %q", got, want)
	}
	if got, want := cfg.Task, "implement feature x"; got != want {
		t.Fatalf("task=%q want %q", got, want)
	}
}

func TestLoadRunConfigFile_ModelDBOpenRouterKeys(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/openrouter.json
  openrouter_model_info_update_policy: pinned
  openrouter_model_info_url: https://openrouter.ai/api/v1/models
  openrouter_model_info_fetch_timeout_ms: 3456
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile(yaml): %v", err)
	}
	if got, want := cfg.ModelDB.OpenRouterModelInfoPath, "/tmp/openrouter.json"; got != want {
		t.Fatalf("openrouter_model_info_path=%q want %q", got, want)
	}
	if got, want := cfg.ModelDB.OpenRouterModelInfoUpdatePolicy, "pinned"; got != want {
		t.Fatalf("openrouter_model_info_update_policy=%q want %q", got, want)
	}
	if got, want := cfg.ModelDB.OpenRouterModelInfoFetchTimeoutMS, 3456; got != want {
		t.Fatalf("openrouter_model_info_fetch_timeout_ms=%d want %d", got, want)
	}
}

func TestNormalizeProviderKey_GeminiMapsToGoogle(t *testing.T) {
	if got := normalizeProviderKey("gemini"); got != "google" {
		t.Fatalf("normalizeProviderKey(gemini)=%q want google", got)
	}
	if got := normalizeProviderKey("GOOGLE"); got != "google" {
		t.Fatalf("normalizeProviderKey(GOOGLE)=%q want google", got)
	}
}

func TestNormalizeProviderKey_DelegatesToProviderSpecAliases(t *testing.T) {
	if got := normalizeProviderKey("z-ai"); got != "zai" {
		t.Fatalf("normalizeProviderKey(z-ai)=%q want zai", got)
	}
	if got := normalizeProviderKey("moonshot"); got != "kimi" {
		t.Fatalf("normalizeProviderKey(moonshot)=%q want kimi", got)
	}
}

func TestLoadRunConfigFile_CXDBAutostartDefaultsAndTrim(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
  autostart:
    enabled: true
    command: ["  sh  ", "", "  -c", " echo ok "]
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile(yaml): %v", err)
	}
	if !cfg.CXDB.Autostart.Enabled {
		t.Fatalf("expected autostart enabled")
	}
	if got, want := cfg.CXDB.Autostart.WaitTimeoutMS, 20000; got != want {
		t.Fatalf("wait_timeout_ms=%d want %d", got, want)
	}
	if got, want := cfg.CXDB.Autostart.PollIntervalMS, 250; got != want {
		t.Fatalf("poll_interval_ms=%d want %d", got, want)
	}
	if got, want := strings.Join(cfg.CXDB.Autostart.Command, " "), "sh -c echo ok"; got != want {
		t.Fatalf("autostart command=%q want %q", got, want)
	}
}

func TestLoadRunConfigFile_CXDBAutostartValidation(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
  autostart:
    enabled: true
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunConfigFile(yml); err == nil || !strings.Contains(err.Error(), "cxdb.autostart.command") {
		t.Fatalf("expected autostart command validation error, got: %v", err)
	}
}

func TestLoadRunConfigFile_CXDBAutostartUIAllowsAutodiscovery(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
  autostart:
    ui:
      enabled: true
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("expected config to load for UI autodiscovery defaults, got: %v", err)
	}
	if !cfg.CXDB.Autostart.UI.Enabled {
		t.Fatalf("expected ui.enabled=true")
	}
}

func TestLoadRunConfigFile_DefaultCLIProfileIsReal(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: cli
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRunConfigFile(yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if got := cfg.LLM.CLIProfile; got != "real" {
		t.Fatalf("cli_profile=%q want real", got)
	}
}

func loadRunConfigFromBytesForTest(t *testing.T, yml []byte) (*RunConfigFile, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.yaml")
	if err := os.WriteFile(p, yml, 0o644); err != nil {
		t.Fatalf("write run.yaml: %v", err)
	}
	return LoadRunConfigFile(p)
}

func TestLoadRunConfigFile_InputMaterializationConfig(t *testing.T) {
	yml := []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
inputs:
  materialize:
    enabled: true
    include:
      - .ai/**
      - C:/Users/me/**/*.md
    default_include:
      - docs/**/*.md
    follow_references: true
    infer_with_llm: true
    llm_provider: openai
    llm_model: gpt-5
`)
	cfg, err := loadRunConfigFromBytesForTest(t, yml)
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	m := cfg.Inputs.Materialize
	if m.Enabled == nil || !*m.Enabled {
		t.Fatal("inputs.materialize.enabled: expected true")
	}
	if got, want := strings.Join(m.Include, ","), ".ai/**,C:/Users/me/**/*.md"; got != want {
		t.Fatalf("inputs.materialize.include: got %q want %q", got, want)
	}
	if got, want := strings.Join(m.DefaultInclude, ","), "docs/**/*.md"; got != want {
		t.Fatalf("inputs.materialize.default_include: got %q want %q", got, want)
	}
	if m.FollowReferences == nil || !*m.FollowReferences {
		t.Fatal("inputs.materialize.follow_references: expected true")
	}
	if m.InferWithLLM == nil || !*m.InferWithLLM {
		t.Fatal("inputs.materialize.infer_with_llm: expected true")
	}
	if got, want := m.LLMProvider, "openai"; got != want {
		t.Fatalf("inputs.materialize.llm_provider: got %q want %q", got, want)
	}
	if got, want := m.LLMModel, "gpt-5"; got != want {
		t.Fatalf("inputs.materialize.llm_model: got %q want %q", got, want)
	}
}

func TestLoadRunConfigFile_InputMaterializationDefaultsAndValidation(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		yml := []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`)
		cfg, err := loadRunConfigFromBytesForTest(t, yml)
		if err != nil {
			t.Fatalf("LoadRunConfigFile: %v", err)
		}
		m := cfg.Inputs.Materialize
		if m.Enabled == nil || !*m.Enabled {
			t.Fatal("inputs.materialize.enabled default: expected true")
		}
		if m.InferWithLLM == nil || *m.InferWithLLM {
			t.Fatal("inputs.materialize.infer_with_llm default: expected false")
		}
		if len(m.DefaultInclude) != 0 {
			t.Fatalf("inputs.materialize.default_include default: got %v want empty", m.DefaultInclude)
		}
	})

	t.Run("infer_requires_model_and_provider", func(t *testing.T) {
		yml := []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
inputs:
  materialize:
    infer_with_llm: true
`)
		_, err := loadRunConfigFromBytesForTest(t, yml)
		if err == nil || !strings.Contains(err.Error(), "inputs.materialize.llm_provider") {
			t.Fatalf("expected llm_provider validation error, got: %v", err)
		}
	})
}

func TestLoadRunConfigFile_InputMaterializationImportsMapping(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/requirements.md
        required: true
      - pattern: docs/context/*.md
        required: false
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if got := strings.Join(cfg.Inputs.Materialize.Include, ","); got != "docs/requirements.md" {
		t.Fatalf("include=%q", got)
	}
	if got := strings.Join(cfg.Inputs.Materialize.DefaultInclude, ","); got != "docs/context/*.md" {
		t.Fatalf("default_include=%q", got)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsConflict(t *testing.T) {
	_, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    include: [docs/required.md]
    imports:
      - pattern: docs/required.md
`))
	if err == nil || !strings.Contains(err.Error(), "failure_reason=input_imports_conflict") {
		t.Fatalf("expected deterministic failure_reason=input_imports_conflict, got %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsDefaultIncludeConflict(t *testing.T) {
	_, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    default_include: [docs/optional/*.md]
    imports:
      - pattern: docs/required.md
`))
	if err == nil || !strings.Contains(err.Error(), "failure_reason=input_imports_conflict") {
		t.Fatalf("expected deterministic failure_reason=input_imports_conflict, got %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsConflict_ExplicitEmptyLegacyFields(t *testing.T) {
	tests := []string{
		`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    include: []
    imports:
      - pattern: docs/required.md
`,
		`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    default_include: []
    imports:
      - pattern: docs/required.md
`,
	}
	for _, yml := range tests {
		_, err := loadRunConfigFromBytesForTest(t, []byte(yml))
		if err == nil || !strings.Contains(err.Error(), "failure_reason=input_imports_conflict") {
			t.Fatalf("expected deterministic failure_reason=input_imports_conflict, got %v", err)
		}
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsPatternRequired(t *testing.T) {
	_, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: "   "
`))
	if err == nil || !strings.Contains(err.Error(), "inputs.materialize.imports[0].pattern") {
		t.Fatalf("expected imports pattern validation error, got %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsRequiredDefaultsTrue(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/required.md
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if got := strings.Join(cfg.Inputs.Materialize.Include, ","); got != "docs/required.md" {
		t.Fatalf("required defaults to include, got %q", got)
	}
	if len(cfg.Inputs.Materialize.DefaultInclude) != 0 {
		t.Fatalf("default_include must be empty, got %v", cfg.Inputs.Materialize.DefaultInclude)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsDedupeFirstSeenOrder(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/a.md
      - pattern: docs/a.md
      - pattern: docs/b.md
        required: false
      - pattern: docs/b.md
        required: false
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if got := strings.Join(cfg.Inputs.Materialize.Include, ","); got != "docs/a.md" {
		t.Fatalf("include dedupe/order mismatch: %q", got)
	}
	if got := strings.Join(cfg.Inputs.Materialize.DefaultInclude, ","); got != "docs/b.md" {
		t.Fatalf("default_include dedupe/order mismatch: %q", got)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsDedupe_FirstRequiredWins(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/shared.md
        required: true
      - pattern: docs/shared.md
        required: false
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if got := strings.Join(cfg.Inputs.Materialize.Include, ","); got != "docs/shared.md" {
		t.Fatalf("include first-seen winner mismatch: %q", got)
	}
	if len(cfg.Inputs.Materialize.DefaultInclude) != 0 {
		t.Fatalf("default_include must not duplicate first-seen pattern, got %v", cfg.Inputs.Materialize.DefaultInclude)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsDedupe_FirstBestEffortWins(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/shared.md
        required: false
      - pattern: docs/shared.md
        required: true
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if len(cfg.Inputs.Materialize.Include) != 0 {
		t.Fatalf("include must remain empty when first-seen import is best-effort, got %v", cfg.Inputs.Materialize.Include)
	}
	if got := strings.Join(cfg.Inputs.Materialize.DefaultInclude, ","); got != "docs/shared.md" {
		t.Fatalf("default_include first-seen winner mismatch: %q", got)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsUnknownFieldRejected(t *testing.T) {
	_, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    imports:
      - pattern: docs/required.md
        extra: nope
`))
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("expected unknown imports field error, got %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationLegacyIncludeDefaultIncludeStillWorks(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    include: [docs/required.md]
    default_include: [docs/optional/*.md]
`))
	if err != nil {
		t.Fatalf("legacy include/default_include must remain valid: %v", err)
	}
	if got := strings.Join(cfg.Inputs.Materialize.Include, ","); got != "docs/required.md" {
		t.Fatalf("include mismatch: %q", got)
	}
	if got := strings.Join(cfg.Inputs.Materialize.DefaultInclude, ","); got != "docs/optional/*.md" {
		t.Fatalf("default_include mismatch: %q", got)
	}
}

func TestLoadRunConfigFile_InputMaterializationFanInPromoteValidation(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{name: "absolute", entry: "/tmp/postmortem_latest.md"},
		{name: "windows-absolute", entry: "C:/tmp/postmortem_latest.md"},
		{name: "dotdot", entry: "../outside.md"},
		{name: "embedded-dotdot", entry: "safe/../escape.md"},
		{name: "empty", entry: "\"   \""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadRunConfigFromBytesForTest(t, []byte(fmt.Sprintf(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    fan_in:
      promote_run_scoped: [%s]
`, tc.entry)))
			if err == nil || !strings.Contains(err.Error(), "promote_run_scoped") {
				t.Fatalf("expected promote_run_scoped validation error, got %v", err)
			}
		})
	}
}

func TestLoadRunConfigFile_InputMaterializationFanInPromoteNormalizeAndDedupe(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    fan_in:
      promote_run_scoped:
        - postmortem_latest.md
        - ./postmortem_latest.md
        - review_final.md
`))
	if err != nil {
		t.Fatalf("expected valid promote_run_scoped list, got %v", err)
	}
	got := strings.Join(cfg.Inputs.Materialize.FanIn.PromoteRunScoped, ",")
	want := "postmortem_latest.md,review_final.md"
	if got != want {
		t.Fatalf("promote_run_scoped normalize+dedupe: got %q want %q", got, want)
	}
}

func TestLoadRunConfigFile_InputMaterializationFanInPromoteAllowsGlob(t *testing.T) {
	_, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
inputs:
  materialize:
    fan_in:
      promote_run_scoped:
        - "**/review_*.md"
`))
	if err != nil {
		t.Fatalf("glob entries must be accepted in config validation: %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationImportsConflict_JSONExplicitEmptyLegacyFields(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "run.json")
	if err := os.WriteFile(p, []byte(`{
  "version": 1,
  "repo": {"path": "/tmp/repo"},
  "cxdb": {"binary_addr":"127.0.0.1:9009","http_base_url":"http://127.0.0.1:9010"},
  "llm": {"providers": {"openai": {"backend":"api"}}},
  "modeldb": {"openrouter_model_info_path":"/tmp/catalog.json"},
  "inputs": {"materialize": {"include": [], "imports": [{"pattern":"docs/required.md"}]}}
}`), 0o644); err != nil {
		t.Fatalf("write run.json: %v", err)
	}
	_, err := LoadRunConfigFile(p)
	if err == nil || !strings.Contains(err.Error(), "failure_reason=input_imports_conflict") {
		t.Fatalf("expected deterministic failure_reason=input_imports_conflict, got %v", err)
	}
}

func TestLoadRunConfigFile_InputMaterializationDefaults_NoImplicitRootAI(t *testing.T) {
	cfg, err := loadRunConfigFromBytesForTest(t, []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
llm: { providers: { openai: { backend: api } } }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
`))
	if err != nil {
		t.Fatalf("LoadRunConfigFile: %v", err)
	}
	if len(cfg.Inputs.Materialize.DefaultInclude) != 0 {
		t.Fatalf("default_include default must be empty, got %v", cfg.Inputs.Materialize.DefaultInclude)
	}
}

func TestLoadRunConfig_CustomAPIProviderRequiresProtocol(t *testing.T) {
	yml := []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
llm:
  providers:
    acme:
      backend: api
`)
	_, err := loadRunConfigFromBytesForTest(t, yml)
	if err == nil || !strings.Contains(err.Error(), "llm.providers.acme.api.protocol") {
		t.Fatalf("expected protocol validation error, got %v", err)
	}
}

func TestLoadRunConfig_KimiAPIProtocolAccepted(t *testing.T) {
	yml := []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
llm:
  providers:
    kimi:
      backend: api
      api:
        protocol: anthropic_messages
        api_key_env: KIMI_API_KEY
        base_url: https://api.kimi.com/coding
        path: /v1/messages
`)
	cfg, err := loadRunConfigFromBytesForTest(t, yml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.LLM.Providers["kimi"].API.Protocol; got != "anthropic_messages" {
		t.Fatalf("protocol not parsed: %q", got)
	}
}

func TestLoadRunConfig_ZAIAliasAcceptedWithAPIProtocol(t *testing.T) {
	yml := []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
llm:
  providers:
    z-ai:
      backend: api
      api:
        protocol: openai_chat_completions
        api_key_env: ZAI_API_KEY
`)
	if _, err := loadRunConfigFromBytesForTest(t, yml); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRunConfig_BackwardCompatibleBuiltinProvidersStillValid(t *testing.T) {
	yml := []byte(`
version: 1
repo: { path: /tmp/repo }
cxdb: { binary_addr: 127.0.0.1:9009, http_base_url: http://127.0.0.1:9010 }
modeldb: { openrouter_model_info_path: /tmp/catalog.json }
llm:
  providers:
    openai: { backend: api }
    anthropic: { backend: api }
    google: { backend: api }
`)
	if _, err := loadRunConfigFromBytesForTest(t, yml); err != nil {
		t.Fatalf("unexpected backward-compat validation error: %v", err)
	}
}

func TestBackwardCompatibility_OpenAIAnthropicGoogleStillValid(t *testing.T) {
	cfg := &RunConfigFile{Version: 1}
	cfg.Repo.Path = "/tmp/repo"
	cfg.CXDB.BinaryAddr = "127.0.0.1:9009"
	cfg.CXDB.HTTPBaseURL = "http://127.0.0.1:9010"
	cfg.ModelDB.OpenRouterModelInfoPath = "/tmp/catalog.json"
	cfg.LLM.Providers = map[string]ProviderConfig{
		"openai":    {Backend: BackendAPI},
		"anthropic": {Backend: BackendAPI},
		"google":    {Backend: BackendAPI},
	}
	applyConfigDefaults(cfg)
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadRunConfigFile_InvalidCLIProfile(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  cli_profile: banana
  providers:
    openai:
      backend: cli
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatalf("expected invalid cli_profile error")
	}
	if !strings.Contains(err.Error(), "invalid llm.cli_profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRunConfigFile_ExecutableOverrideRequiresTestShim(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  cli_profile: real
  providers:
    openai:
      backend: cli
      executable: /tmp/fake/codex
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatalf("expected executable override validation error")
	}
	if !strings.Contains(err.Error(), "llm.providers.openai.executable") || !strings.Contains(err.Error(), "test_shim") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRunConfigFile_InvalidPromptProbeTransport(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
preflight:
  prompt_probes:
    transports: ["strem"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatal("expected invalid prompt probe transport validation error")
	}
	if !strings.Contains(err.Error(), "preflight.prompt_probes.transports") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRunConfigFile_InvalidPromptProbeNumericPolicy(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "run.yaml")
	if err := os.WriteFile(yml, []byte(`
version: 1
repo:
  path: /tmp/repo
cxdb:
  binary_addr: 127.0.0.1:9009
  http_base_url: http://127.0.0.1:9010
llm:
  providers:
    openai:
      backend: api
modeldb:
  openrouter_model_info_path: /tmp/catalog.json
preflight:
  prompt_probes:
    timeout_ms: -1
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadRunConfigFile(yml)
	if err == nil {
		t.Fatal("expected invalid prompt probe numeric policy validation error")
	}
	if !strings.Contains(err.Error(), "preflight.prompt_probes.timeout_ms") {
		t.Fatalf("unexpected error: %v", err)
	}
}
