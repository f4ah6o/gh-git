package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitContainsProfileSelection(t *testing.T) {
	for _, name := range []string{"bash", "zsh", "fish"} {
		output, err := Init(name)
		if err != nil {
			t.Fatalf("Init(%q): %v", name, err)
		}
		if !strings.Contains(output, ".gh-git-profile") || !strings.Contains(output, "GH_CONFIG_DIR") {
			t.Fatalf("Init(%q) does not select the generated profile", name)
		}
	}
}

func TestInitResolvesProfileFromCommonGitDir(t *testing.T) {
	for _, name := range []string{"bash", "zsh", "fish"} {
		output, err := Init(name)
		if err != nil {
			t.Fatalf("Init(%q): %v", name, err)
		}
		if !strings.Contains(output, "--git-common-dir") {
			t.Fatalf("Init(%q) does not resolve the common git dir", name)
		}
		if strings.Contains(output, "gh-git/gh-config 2>/dev/null") {
			t.Fatalf("Init(%q) resolves the profile inside the per-worktree git dir", name)
		}
	}
}

func TestBashHookFindsProfileFromLinkedWorktree(t *testing.T) {
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	worktree := filepath.Join(tmp, "worktree")
	subdir := filepath.Join(root, "sub")
	for _, dir := range []string{root, subdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "HOME="+tmp, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(tmp, "global.gitconfig"))
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(root, "init", "-q")
	runGit(root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-qm", "init", "--allow-empty")
	runGit(root, "worktree", "add", "-q", worktree)

	profileDir := filepath.Join(root, ".git", "gh-git", "gh-config")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, ".gh-git-profile"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	hook, err := Init("bash")
	if err != nil {
		t.Fatal(err)
	}
	script := hook + "\n" + strings.Join([]string{
		fmt.Sprintf("cd %q", worktree),
		"gh_git_apply",
		`printf 'worktree=%s\n' "$GH_CONFIG_DIR"`,
		fmt.Sprintf("cd %q", subdir),
		"gh_git_apply",
		`printf 'subdir=%s\n' "$GH_CONFIG_DIR"`,
		"cd /",
		"gh_git_apply",
		`printf 'outside=%s\n' "${GH_CONFIG_DIR-unset}"`,
	}, "\n")
	cmd := exec.Command(bashPath, "-c", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash hook: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	want := []string{
		"worktree=" + profileDir,
		"subdir=" + profileDir,
		"outside=unset",
	}
	if len(lines) != len(want) {
		t.Fatalf("hook output = %q, want %v", out, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("hook output = %q, want %v", out, want)
		}
	}
}
