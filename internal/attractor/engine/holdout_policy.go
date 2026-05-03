package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/danshapiro/kilroy/internal/attractor/model"
	"github.com/danshapiro/kilroy/internal/attractor/runtime"
)

type ResolvedHoldoutPolicy struct {
	Sets []ResolvedHoldoutSet `json:"sets"`
}

type ResolvedHoldoutSet struct {
	Name              string           `json:"name"`
	Paths             []string         `json:"paths"`
	VisibleNodes      map[string]bool  `json:"visible_nodes,omitempty"`
	VisibleClasses    map[string]bool  `json:"visible_classes,omitempty"`
	ScanWorktreeGlobs []string         `json:"scan_worktree_globs,omitempty"`
	DenyPatterns      []string         `json:"deny_patterns,omitempty"`
	denyRegexps       []*regexp.Regexp `json:"-"`
}

type holdoutNodeSession struct {
	nodeID      string
	stageDir    string
	worktreeDir string
	runID       string
	hiddenSets  []string
	visibleSets []string
	snapshots   []holdoutFileSnapshot
	scanResult  holdoutScanResult
	restored    bool
}

type holdoutFileSnapshot struct {
	SetName string      `json:"set_name"`
	Path    string      `json:"path"`
	Mode    fs.FileMode `json:"-"`
	Content []byte      `json:"-"`
}

type holdoutReport struct {
	NodeID      string            `json:"node_id"`
	HiddenSets  []string          `json:"hidden_sets,omitempty"`
	VisibleSets []string          `json:"visible_sets,omitempty"`
	HiddenFiles []string          `json:"hidden_files,omitempty"`
	Scan        holdoutScanResult `json:"scan"`
	Restored    bool              `json:"restored"`
}

type holdoutScanResult struct {
	Passed bool                   `json:"passed"`
	Hits   []holdoutContamination `json:"hits,omitempty"`
}

type holdoutContamination struct {
	SetName string `json:"set_name"`
	Pattern string `json:"pattern"`
	File    string `json:"file"`
}

func validateVisibilityConfig(cfg *RunConfigFile) error {
	if cfg == nil {
		return nil
	}
	for rawName, holdout := range cfg.Visibility.Holdouts {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return fmt.Errorf("visibility.holdouts contains an empty holdout name")
		}
		if len(holdout.Paths) == 0 {
			return fmt.Errorf("visibility.holdouts.%s.paths must contain at least one path", name)
		}
		for _, p := range holdout.Paths {
			if _, err := normalizeHoldoutPathSpec(p); err != nil {
				return fmt.Errorf("visibility.holdouts.%s.paths: %w", name, err)
			}
		}
		for _, pattern := range holdout.Scan.DenyPatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("visibility.holdouts.%s.scan.deny_patterns contains invalid regexp %q: %w", name, pattern, err)
			}
		}
	}
	return nil
}

func ResolveHoldoutPolicy(cfg *RunConfigFile, g *model.Graph, repoPath string) (*ResolvedHoldoutPolicy, error) {
	if cfg == nil || len(cfg.Visibility.Holdouts) == 0 {
		return nil, nil
	}
	repoPath = strings.TrimSpace(firstNonEmpty(repoPath, cfg.Repo.Path))
	if repoPath == "" {
		return nil, fmt.Errorf("visibility.holdouts requires repo.path")
	}
	if err := ensureGitWorktree(repoPath); err != nil {
		return nil, fmt.Errorf("visibility.holdouts requires a git repo: %w", err)
	}

	names := make([]string, 0, len(cfg.Visibility.Holdouts))
	for name := range cfg.Visibility.Holdouts {
		names = append(names, name)
	}
	sort.Strings(names)

	policy := &ResolvedHoldoutPolicy{}
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		holdout := cfg.Visibility.Holdouts[rawName]
		set := ResolvedHoldoutSet{
			Name:              name,
			VisibleNodes:      map[string]bool{},
			VisibleClasses:    map[string]bool{},
			ScanWorktreeGlobs: append([]string(nil), holdout.Scan.WorktreeGlobs...),
			DenyPatterns:      append([]string(nil), holdout.Scan.DenyPatterns...),
		}
		trackedFiles, err := listTrackedFiles(repoPath)
		if err != nil {
			return nil, fmt.Errorf("visibility.holdouts.%s could not list tracked files: %w", name, err)
		}
		seenPath := map[string]bool{}
		for _, p := range holdout.Paths {
			resolved, err := resolveHoldoutPaths(p, trackedFiles)
			if err != nil {
				return nil, fmt.Errorf("visibility.holdouts.%s.paths: %w", name, err)
			}
			for _, rel := range resolved {
				if seenPath[rel] {
					continue
				}
				seenPath[rel] = true
				set.Paths = append(set.Paths, rel)
			}
		}
		sort.Strings(set.Paths)
		for _, nodeID := range holdout.Visible.Nodes {
			nodeID = strings.TrimSpace(nodeID)
			if nodeID == "" {
				continue
			}
			if g == nil || g.Nodes[nodeID] == nil {
				return nil, fmt.Errorf("visibility.holdouts.%s.visible.nodes references unknown node %q", name, nodeID)
			}
			set.VisibleNodes[nodeID] = true
		}
		for _, className := range holdout.Visible.Classes {
			className = strings.TrimSpace(className)
			if className == "" {
				continue
			}
			if !graphHasClass(g, className) {
				return nil, fmt.Errorf("visibility.holdouts.%s.visible.classes references unknown class %q", name, className)
			}
			set.VisibleClasses[className] = true
		}
		for _, pattern := range set.DenyPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("visibility.holdouts.%s.scan.deny_patterns contains invalid regexp %q: %w", name, pattern, err)
			}
			set.denyRegexps = append(set.denyRegexps, re)
		}
		policy.Sets = append(policy.Sets, set)
	}
	return policy, nil
}

func normalizeHoldoutPathSpec(raw string) (string, error) {
	p := filepath.ToSlash(strings.TrimSpace(raw))
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be repo-relative", raw)
	}
	cleaned := path.Clean(p)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("path %q must stay inside the repo", raw)
	}
	if holdoutContainsGlobMeta(p) {
		p = strings.TrimPrefix(p, "./")
		if !doublestar.ValidatePattern(p) {
			return "", fmt.Errorf("path %q is not a valid glob pattern", raw)
		}
		return p, nil
	}
	return strings.TrimSuffix(cleaned, "/"), nil
}

func holdoutContainsGlobMeta(value string) bool {
	return strings.ContainsAny(value, "*?[{")
}

func resolveHoldoutPaths(raw string, trackedFiles []string) ([]string, error) {
	spec, err := normalizeHoldoutPathSpec(raw)
	if err != nil {
		return nil, err
	}
	if holdoutContainsGlobMeta(spec) {
		return resolveHoldoutGlob(spec, trackedFiles)
	}

	for _, tracked := range trackedFiles {
		if tracked == spec {
			return []string{spec}, nil
		}
	}

	prefix := strings.TrimSuffix(spec, "/") + "/"
	var matches []string
	for _, tracked := range trackedFiles {
		if strings.HasPrefix(tracked, prefix) {
			matches = append(matches, tracked)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("path %q must match at least one tracked file or directory", raw)
	}
	return matches, nil
}

func resolveHoldoutGlob(pattern string, trackedFiles []string) ([]string, error) {
	var matches []string
	for _, tracked := range trackedFiles {
		ok, err := doublestar.Match(pattern, tracked)
		if err != nil {
			return nil, fmt.Errorf("glob %q is invalid: %w", pattern, err)
		}
		if ok {
			matches = append(matches, tracked)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("glob %q must match at least one tracked file", pattern)
	}
	return matches, nil
}

func listTrackedFiles(repoPath string) ([]string, error) {
	out, err := exec.Command("git", "-C", repoPath, "ls-files", "-z").Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, part := range bytes.Split(out, []byte{0}) {
		file := strings.TrimSpace(string(part))
		if file == "" {
			continue
		}
		files = append(files, filepath.ToSlash(file))
	}
	sort.Strings(files)
	return files, nil
}

func graphHasClass(g *model.Graph, className string) bool {
	if g == nil {
		return false
	}
	for _, n := range g.Nodes {
		for _, c := range n.ClassList() {
			if c == className {
				return true
			}
		}
	}
	return false
}

func ensureGitWorktree(repoPath string) error {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("%s is not inside a git worktree", repoPath)
	}
	return nil
}

func ensureGitTracked(repoPath string, rel string) error {
	out, err := exec.Command("git", "-C", repoPath, "ls-files", "--error-unmatch", "--", rel).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func (p *ResolvedHoldoutPolicy) visibleForNode(set ResolvedHoldoutSet, node *model.Node) bool {
	if p == nil || node == nil {
		return false
	}
	if set.VisibleNodes[node.ID] {
		return true
	}
	for _, c := range node.ClassList() {
		if set.VisibleClasses[c] {
			return true
		}
	}
	return false
}

func (e *Engine) ensureHoldoutPolicy() error {
	if e == nil {
		return nil
	}
	if e.HoldoutPolicy != nil {
		if e.LogsRoot != "" {
			_ = writeJSON(filepath.Join(e.LogsRoot, "holdouts_manifest.json"), e.HoldoutPolicy)
		}
		return nil
	}
	policy, err := ResolveHoldoutPolicy(e.RunConfig, e.Graph, e.Options.RepoPath)
	if err != nil {
		return err
	}
	e.HoldoutPolicy = policy
	if policy != nil && e.LogsRoot != "" {
		_ = writeJSON(filepath.Join(e.LogsRoot, "holdouts_manifest.json"), policy)
	}
	return nil
}

func (e *Engine) beginHoldoutsForNode(node *model.Node, stageDir string) (*holdoutNodeSession, error) {
	if err := e.ensureHoldoutPolicy(); err != nil {
		return nil, err
	}
	if e.HoldoutPolicy == nil || len(e.HoldoutPolicy.Sets) == 0 {
		return nil, nil
	}
	session := &holdoutNodeSession{
		nodeID:      node.ID,
		stageDir:    stageDir,
		worktreeDir: e.WorktreeDir,
		runID:       e.Options.RunID,
		scanResult:  holdoutScanResult{Passed: true},
	}
	for _, set := range e.HoldoutPolicy.Sets {
		if e.HoldoutPolicy.visibleForNode(set, node) {
			session.visibleSets = append(session.visibleSets, set.Name)
			for _, rel := range set.Paths {
				if err := ensureHoldoutVisible(e.WorktreeDir, rel); err != nil {
					return session, err
				}
			}
			continue
		}
		session.hiddenSets = append(session.hiddenSets, set.Name)
		for _, rel := range set.Paths {
			snap, err := hideHoldoutFile(e.WorktreeDir, set.Name, rel)
			if err != nil {
				return session, err
			}
			session.snapshots = append(session.snapshots, snap)
		}
	}
	return session, nil
}

func ensureHoldoutVisible(worktreeDir, rel string) error {
	if err := runGit(context.Background(), worktreeDir, "update-index", "--no-skip-worktree", "--", rel); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, filepath.FromSlash(rel))); err == nil {
		return nil
	}
	return runGit(context.Background(), worktreeDir, "checkout", "--", rel)
}

func hideHoldoutFile(worktreeDir, setName, rel string) (holdoutFileSnapshot, error) {
	full := filepath.Join(worktreeDir, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		return holdoutFileSnapshot{}, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return holdoutFileSnapshot{}, err
	}
	if err := runGit(context.Background(), worktreeDir, "update-index", "--skip-worktree", "--", rel); err != nil {
		return holdoutFileSnapshot{}, err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		_ = runGit(context.Background(), worktreeDir, "update-index", "--no-skip-worktree", "--", rel)
		return holdoutFileSnapshot{}, err
	}
	return holdoutFileSnapshot{SetName: setName, Path: rel, Mode: info.Mode(), Content: b}, nil
}

func (s *holdoutNodeSession) restore() error {
	if s == nil || s.restored {
		return nil
	}
	var errs []string
	for _, snap := range s.snapshots {
		full := filepath.Join(s.worktreeDir, filepath.FromSlash(snap.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := os.WriteFile(full, snap.Content, snap.Mode); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := runGit(context.Background(), s.worktreeDir, "update-index", "--no-skip-worktree", "--", snap.Path); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.restored = true
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (s *holdoutNodeSession) scan(policy *ResolvedHoldoutPolicy) holdoutScanResult {
	result := holdoutScanResult{Passed: true}
	if s == nil || policy == nil || len(s.hiddenSets) == 0 {
		return result
	}
	hidden := map[string]bool{}
	for _, name := range s.hiddenSets {
		hidden[name] = true
	}
	for _, set := range policy.Sets {
		if !hidden[set.Name] || len(set.denyRegexps) == 0 {
			continue
		}
		result.Hits = append(result.Hits, scanTreeForHoldoutContamination(set, s.stageDir, "stage:"+filepath.Base(s.stageDir), nil)...)
		for _, glob := range set.ScanWorktreeGlobs {
			result.Hits = append(result.Hits, scanWorktreeGlobForHoldoutContamination(set, s.worktreeDir, s.runID, glob)...)
		}
	}
	if len(result.Hits) > 0 {
		result.Passed = false
	}
	return result
}

func scanTreeForHoldoutContamination(set ResolvedHoldoutSet, root string, label string, filter func(rel string) bool) []holdoutContamination {
	var hits []holdoutContamination
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = filepath.Base(p)
		}
		rel = filepath.ToSlash(rel)
		if filter != nil && !filter(rel) {
			return nil
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		for i, re := range set.denyRegexps {
			if re.Match(b) {
				hits = append(hits, holdoutContamination{
					SetName: set.Name,
					Pattern: set.DenyPatterns[i],
					File:    label + "/" + rel,
				})
			}
		}
		return nil
	})
	return hits
}

func scanWorktreeGlobForHoldoutContamination(set ResolvedHoldoutSet, worktreeDir, runID, glob string) []holdoutContamination {
	pattern := expandHoldoutGlob(glob, runID)
	filter := func(rel string) bool {
		return matchHoldoutGlob(pattern, rel)
	}
	return scanTreeForHoldoutContamination(set, worktreeDir, "worktree", filter)
}

func expandHoldoutGlob(glob string, runID string) string {
	out := filepath.ToSlash(strings.TrimSpace(glob))
	out = strings.ReplaceAll(out, "${KILROY_RUN_ID}", runID)
	out = strings.ReplaceAll(out, "$KILROY_RUN_ID", runID)
	return strings.TrimPrefix(out, "./")
}

func matchHoldoutGlob(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(pattern), "./"))
	rel = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(rel), "./"))
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}
	if ok, err := path.Match(pattern, rel); err == nil && ok {
		return true
	}
	return pattern == rel
}

func (s *holdoutNodeSession) writeReport() {
	if s == nil || s.stageDir == "" {
		return
	}
	hiddenFiles := make([]string, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		hiddenFiles = append(hiddenFiles, snap.Path)
	}
	_ = writeJSON(filepath.Join(s.stageDir, "holdout_report.json"), holdoutReport{
		NodeID:      s.nodeID,
		HiddenSets:  append([]string(nil), s.hiddenSets...),
		VisibleSets: append([]string(nil), s.visibleSets...),
		HiddenFiles: hiddenFiles,
		Scan:        s.scanResult,
		Restored:    s.restored,
	})
}

func applyHoldoutContamination(out runtime.Outcome, scan holdoutScanResult) runtime.Outcome {
	if scan.Passed || len(scan.Hits) == 0 {
		return out
	}
	out.Status = runtime.StatusFail
	out.FailureReason = "holdout_contamination: implementation-facing artifacts mention hidden holdout material"
	if out.Meta == nil {
		out.Meta = map[string]any{}
	}
	out.Meta["holdout_contamination"] = scan.Hits
	if out.ContextUpdates == nil {
		out.ContextUpdates = map[string]any{}
	}
	out.ContextUpdates["failure_reason"] = out.FailureReason
	return out
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return nil
}
