package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// executeSetupCommands runs the configured setup commands sequentially in the
// worktree directory before the first pipeline node executes. Commands are run
// via "sh -c" and fail fast on the first error.
func (e *Engine) executeSetupCommands(ctx context.Context) error {
	if e == nil || e.RunConfig == nil || len(e.RunConfig.Setup.Commands) == 0 {
		return nil
	}

	timeoutMS := e.RunConfig.Setup.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 300000
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for i, cmdStr := range e.RunConfig.Setup.Commands {
		cmdStr = strings.TrimSpace(cmdStr)
		if cmdStr == "" {
			continue
		}

		e.appendProgress(map[string]any{
			"event":   "setup_command_start",
			"index":   i,
			"command": cmdStr,
		})
		start := time.Now()

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		cmd.Dir = e.WorktreeDir
		// Run in its own process group so we can kill the entire tree on timeout.
		setProcessGroupAttr(cmd)
		cmd.Cancel = func() error {
			return forceKillPIDTree(cmd.Process.Pid)
		}
		cmd.WaitDelay = 3 * time.Second
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		durationMS := time.Since(start).Milliseconds()
		if errors.Is(err, exec.ErrWaitDelay) {
			e.appendProgress(map[string]any{
				"event":       "setup_command_ok",
				"index":       i,
				"command":     cmdStr,
				"duration_ms": durationMS,
				"stdout":      strings.TrimSpace(stdout.String()),
				"warning":     "child process held I/O pipes open past WaitDelay; treated as success",
			})
		} else if err != nil {
			e.appendProgress(map[string]any{
				"event":       "setup_command_failed",
				"index":       i,
				"command":     cmdStr,
				"duration_ms": durationMS,
				"error":       err.Error(),
				"stdout":      strings.TrimSpace(stdout.String()),
				"stderr":      strings.TrimSpace(stderr.String()),
			})
			return fmt.Errorf("setup command [%d] %q failed: %w", i, cmdStr, err)
		} else {
			e.appendProgress(map[string]any{
				"event":       "setup_command_ok",
				"index":       i,
				"command":     cmdStr,
				"duration_ms": durationMS,
				"stdout":      strings.TrimSpace(stdout.String()),
			})
		}
	}

	return nil
}
