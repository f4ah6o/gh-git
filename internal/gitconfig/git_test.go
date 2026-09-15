package gitconfig

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalConfigDoesNotUseGlobalAuthor(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git := New()
	git.WorkDir = root
	git.Env = append(os.Environ(), "HOME="+tmp, "GIT_CONFIG_NOSYSTEM=1")
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = git.Env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "--global", "user.name", "global")
	run("config", "--local", "user.name", "local")

	got, err := git.EffectiveValues(context.Background(), root, "user.name")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[len(got)-1] != "local" {
		t.Fatalf("effective author = %#v", got)
	}
}

func TestRemotes(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git := New()
	git.WorkDir = root
	git.Env = append(os.Environ(), "HOME="+tmp, "GIT_CONFIG_NOSYSTEM=1")
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@github.com:example/gh-git.git"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = git.Env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	remotes, err := git.Remotes(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0].Host != "github.com" || !remotes[0].SSH {
		t.Fatalf("remotes = %#v", remotes)
	}
}
