package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// GitExitError reports an exit status produced by the real git process.
// Stderr is already forwarded directly, so callers should normally preserve
// this status without adding an extra gh-git error message.
type GitExitError struct {
	Status int
}

func (e *GitExitError) Error() string {
	return fmt.Sprintf("git exited with status %d", e.Status)
}

func (e *GitExitError) ExitCode() int {
	if e.Status > 0 {
		return e.Status
	}
	return 1
}

// RunGit invokes the real git executable without a shell and preserves argv,
// stdin, stdout, stderr, environment, working directory, and Git's exit code.
func (a *App) RunGit(ctx context.Context, args []string, input io.Reader) error {
	binary := "git"
	dir := ""
	var env []string
	if a.Git != nil {
		if a.Git.Binary != "" {
			binary = a.Git.Binary
		}
		dir = a.Git.WorkDir
		env = a.Git.Env
	}

	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	if env != nil {
		command.Env = env
	}
	command.Stdin = input
	command.Stdout = a.Out
	command.Stderr = a.Err

	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &GitExitError{Status: exitErr.ExitCode()}
		}
		return fmt.Errorf("run git: %w", err)
	}
	return nil
}

// ExitCode extracts a process exit status that should be returned unchanged by
// the top-level executable. It returns false for ordinary gh-git errors.
func ExitCode(err error) (int, bool) {
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode(), true
	}
	return 0, false
}
