package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/f4ah6o/gh-git/internal/ghauth"
	"github.com/f4ah6o/gh-git/internal/gitconfig"
	"github.com/f4ah6o/gh-git/internal/identity"
)

type fakeAuth struct {
	profiles map[string]identity.Profile
	tokens   map[string]string
}

func (f fakeAuth) key(host, account string) string { return host + "\x00" + account }

func (f fakeAuth) Token(_ context.Context, host, account string) (string, error) {
	if token := f.tokens[f.key(host, account)]; token != "" {
		return token, nil
	}
	return "", os.ErrNotExist
}

func (f fakeAuth) Profile(_ context.Context, host, account string) (identity.Profile, error) {
	if profile, ok := f.profiles[f.key(host, account)]; ok {
		return profile, nil
	}
	return identity.Profile{}, os.ErrNotExist
}

func (f fakeAuth) Accounts(_ context.Context, host string) ([]ghauth.Account, error) {
	return []ghauth.Account{{Host: host, Login: "github-username", Active: true, State: "success", TokenSource: "keyring"}}, nil
}

func newTestRepo(t *testing.T, remote, name, email string) (*gitconfig.Client, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append([]string{}, os.Environ()...)
	env = append(env, "HOME="+tmp, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(tmp, "global.gitconfig"))
	for _, args := range [][]string{{"init", "-q"}, {"config", "--local", "user.name", name}, {"config", "--local", "user.email", email}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if remote != "" {
		cmd := exec.Command("git", "-C", root, "remote", "add", "origin", remote)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git remote: %v\n%s", err, out)
		}
	}
	git := gitconfig.New()
	git.WorkDir = root
	git.Env = env
	return git, root
}

func testApp(git *gitconfig.Client, auth Auth) (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return &App{Git: git, Auth: auth, Out: &out, Err: &errOut}, &out, &errOut
}

func TestMainHelpDescribesQuickStart(t *testing.T) {
	application, out, _ := testApp(gitconfig.New(), fakeAuth{})
	if err := application.Run(context.Background(), []string{"-h"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{
		"gh git — Git with repository-scoped GitHub identity",
		"gh git --version",
		"gh git <git arguments...>",
		"gh git status",
		"gh git bind <github-username>",
		"gh git binding status",
		"gh git shell-init bash",
		"gh git doctor",
		"Remote gh extension install requires a published CalVer Release",
		"never calls gh auth switch",
		"does not add confirmation or reinterpret arguments",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help does not contain %q:\n%s", want, help)
		}
	}
}

func TestVersionFlagPrintsGhGitVersion(t *testing.T) {
	application, out, _ := testApp(gitconfig.New(), fakeAuth{})
	application.Version = "2026.9.16"
	if err := application.Run(context.Background(), []string{"--version"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "gh-git 2026.9.16\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionFlagFallsBackToDevelopmentVersion(t *testing.T) {
	application, out, _ := testApp(gitconfig.New(), fakeAuth{})
	if err := application.Run(context.Background(), []string{"--version"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "gh-git devel\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVersionSubcommandAndDashVersionUseGit(t *testing.T) {
	git, _ := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	ctx := context.Background()

	for _, args := range [][]string{{"version"}, {"--", "--version"}} {
		out.Reset()
		errOut.Reset()
		if err := application.Run(ctx, args, strings.NewReader("")); err != nil {
			t.Fatalf("gh git %s: %v\nstderr: %s", strings.Join(args, " "), err, errOut.String())
		}
		if got := out.String(); !strings.HasPrefix(got, "git version ") {
			t.Fatalf("gh git %s output = %q, want git version output", strings.Join(args, " "), got)
		}
	}
}

func TestGitPassthroughRepresentativeReadWriteCommands(t *testing.T) {
	git, root := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	ctx := context.Background()

	fileName := "file with space.txt"
	filePath := filepath.Join(root, fileName)
	if err := os.WriteFile(filePath, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := application.Run(ctx, []string{"add", "--", fileName}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git add: %v\nstderr: %s", err, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"status", "--short", "-z"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git status: %v\nstderr: %s", err, errOut.String())
	}
	if got, want := out.String(), "A  "+fileName+"\x00"; got != want {
		t.Fatalf("status output = %q, want %q", got, want)
	}

	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"commit", "-m", "test commit"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git commit: %v\nstderr: %s", err, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"log", "-1", "--format=%s"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git log: %v\nstderr: %s", err, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != "test commit" {
		t.Fatalf("commit subject = %q", got)
	}

	if err := os.WriteFile(filePath, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	err := application.Run(ctx, []string{"diff", "--exit-code", "--", fileName}, strings.NewReader(""))
	if err == nil {
		t.Fatal("gh git diff --exit-code unexpectedly succeeded")
	}
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Fatalf("diff exit = (%d, %v), err = %v", code, ok, err)
	}
	if !strings.Contains(out.String(), "-first") || !strings.Contains(out.String(), "+second") {
		t.Fatalf("diff output did not reach stdout: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"rev-parse", "--is-inside-work-tree"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git rev-parse: %v\nstderr: %s", err, errOut.String())
	}
	if strings.TrimSpace(out.String()) != "true" {
		t.Fatalf("rev-parse output = %q", out.String())
	}
}

func TestGitPassthroughLocalRemotePushFetchPull(t *testing.T) {
	git, root := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	ctx := context.Background()
	run := func(args ...string) string {
		t.Helper()
		out.Reset()
		errOut.Reset()
		if err := application.Run(ctx, args, strings.NewReader("")); err != nil {
			t.Fatalf("gh git %s: %v\nstderr: %s", strings.Join(args, " "), err, errOut.String())
		}
		return out.String()
	}

	tracked := filepath.Join(root, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "--", "tracked.txt")
	run("commit", "-m", "initial")
	run("branch", "-M", "main")

	parent := filepath.Dir(root)
	remote := filepath.Join(parent, "remote.git")
	run("init", "--bare", "-q", remote)
	run("remote", "add", "origin", remote)
	run("push", "-u", "origin", "main")
	run("--git-dir="+remote, "symbolic-ref", "HEAD", "refs/heads/main")

	git.WorkDir = parent
	run("clone", "-q", remote, "peer")
	peer := filepath.Join(parent, "peer")
	git.WorkDir = peer
	run("config", "user.name", "Peer User")
	run("config", "user.email", "peer@example.invalid")
	if err := os.WriteFile(filepath.Join(peer, "tracked.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "--", "tracked.txt")
	run("commit", "-m", "peer update")
	run("push", "origin", "main")
	peerHead := strings.TrimSpace(run("rev-parse", "HEAD"))

	git.WorkDir = root
	localHead := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("fetch", "origin")
	remoteHead := strings.TrimSpace(run("rev-parse", "origin/main"))
	if localHead == remoteHead {
		t.Fatalf("fetch did not expose a newer remote commit: local=%s remote=%s", localHead, remoteHead)
	}
	if remoteHead != peerHead {
		t.Fatalf("origin/main = %s, peer HEAD = %s", remoteHead, peerHead)
	}
	run("pull", "--ff-only", "origin", "main")
	if got := strings.TrimSpace(run("rev-parse", "HEAD")); got != peerHead {
		t.Fatalf("HEAD after pull = %s, want %s", got, peerHead)
	}
}

func TestGitPassthroughDashEscapeAndUnknownSubcommand(t *testing.T) {
	git, _ := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	ctx := context.Background()

	if err := application.Run(ctx, []string{"--", "status", "--short"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git -- status: %v\nstderr: %s", err, errOut.String())
	}
	if out.String() != "" {
		t.Fatalf("clean status output = %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	err := application.Run(ctx, []string{"definitely-not-a-git-command"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("unknown git subcommand unexpectedly succeeded")
	}
	if code, ok := ExitCode(err); !ok || code == 0 {
		t.Fatalf("unknown command exit = (%d, %v), err = %v", code, ok, err)
	}
	if errOut.Len() == 0 {
		t.Fatal("git stderr was not forwarded")
	}
}

func TestGitCredentialIsPassthroughUnlessManaged(t *testing.T) {
	git, _ := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	input := "protocol=https\nhost=example.invalid\nusername=test-user\npassword=test-password\n\n"

	if err := application.Run(context.Background(), []string{"credential", "fill"}, strings.NewReader(input)); err != nil {
		t.Fatalf("gh git credential fill: %v\nstderr: %s", err, errOut.String())
	}
	for _, want := range []string{"protocol=https", "host=example.invalid", "username=test-user", "password=test-password"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("git credential output missing %q: %q", want, out.String())
		}
	}
}

func TestGitStatusAndInitUseGitWhileBindingStatusUsesGhGit(t *testing.T) {
	git, root := newTestRepo(t, "", "Test User", "test@example.invalid")
	application, out, errOut := testApp(git, fakeAuth{})
	ctx := context.Background()

	if err := application.Run(ctx, []string{"binding", "status"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "Binding: unbound") {
		t.Fatalf("binding status output = %q", got)
	}

	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"status", "--short"}, strings.NewReader("")); err != nil {
		t.Fatalf("git status: %v\nstderr: %s", err, errOut.String())
	}
	if strings.Contains(out.String(), "Binding:") {
		t.Fatalf("git status was routed to gh-git diagnostics: %q", out.String())
	}

	parent := filepath.Dir(root)
	git.WorkDir = parent
	out.Reset()
	errOut.Reset()
	if err := application.Run(ctx, []string{"init", "-q", "initialized-by-gh-git"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git init: %v\nstderr: %s", err, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(parent, "initialized-by-gh-git", ".git")); err != nil {
		t.Fatalf("git init did not create repository: %v", err)
	}
}

func TestGitPassthroughFetchPullPushAgainstLocalBareRemote(t *testing.T) {
	ctx := context.Background()
	remoteRoot := t.TempDir()
	remote := filepath.Join(remoteRoot, "remote.git")
	cmd := exec.Command("git", "init", "--bare", "-q", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	git1, root1 := newTestRepo(t, remote, "Writer One", "writer1@example.invalid")
	app1, _, errOut1 := testApp(git1, fakeAuth{})
	file1 := filepath.Join(root1, "shared.txt")
	if err := os.WriteFile(file1, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "--", "shared.txt"},
		{"commit", "-m", "initial"},
		{"push", "-u", "origin", "HEAD:main"},
	} {
		errOut1.Reset()
		if err := app1.Run(ctx, args, strings.NewReader("")); err != nil {
			t.Fatalf("gh git %v: %v\nstderr: %s", args, err, errOut1.String())
		}
	}

	git2, root2 := newTestRepo(t, remote, "Writer Two", "writer2@example.invalid")
	app2, _, errOut2 := testApp(git2, fakeAuth{})
	for _, args := range [][]string{
		{"fetch", "origin"},
		{"switch", "-C", "main", "origin/main"},
		{"branch", "--set-upstream-to=origin/main", "main"},
	} {
		errOut2.Reset()
		if err := app2.Run(ctx, args, strings.NewReader("")); err != nil {
			t.Fatalf("gh git %v: %v\nstderr: %s", args, err, errOut2.String())
		}
	}
	data, err := os.ReadFile(filepath.Join(root2, "shared.txt"))
	if err != nil || string(data) != "one\n" {
		t.Fatalf("fetched file = %q, err = %v", data, err)
	}

	if err := os.WriteFile(file1, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "--", "shared.txt"},
		{"commit", "-m", "update"},
		{"push", "origin", "HEAD:main"},
	} {
		errOut1.Reset()
		if err := app1.Run(ctx, args, strings.NewReader("")); err != nil {
			t.Fatalf("gh git %v: %v\nstderr: %s", args, err, errOut1.String())
		}
	}

	errOut2.Reset()
	if err := app2.Run(ctx, []string{"pull", "--ff-only"}, strings.NewReader("")); err != nil {
		t.Fatalf("gh git pull --ff-only: %v\nstderr: %s", err, errOut2.String())
	}
	data, err = os.ReadFile(filepath.Join(root2, "shared.txt"))
	if err != nil || string(data) != "two\n" {
		t.Fatalf("pulled file = %q, err = %v", data, err)
	}
}

func TestBindCredentialAndUnbind(t *testing.T) {
	git, root := newTestRepo(t, "https://github.com/example/gh-git.git", "prior name", "prior@example.invalid")
	auth := fakeAuth{
		profiles: map[string]identity.Profile{"github.com\x00github-username": {ID: 42, Login: "github-username", Name: "Test User"}},
		tokens:   map[string]string{"github.com\x00github-username": "secret-token"},
	}
	application, out, _ := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"bind", "github-username"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret-token") {
		t.Fatal("bind output leaked a token")
	}

	identityValues, err := git.LocalValues(context.Background(), root, gitconfig.IdentityKey)
	if err != nil || len(identityValues) != 1 || identityValues[0] != "github-username" {
		t.Fatalf("identity = %#v, err = %v", identityValues, err)
	}
	nameValues, _ := git.LocalValues(context.Background(), root, "user.name")
	emailValues, _ := git.LocalValues(context.Background(), root, "user.email")
	if !valuesEqual(nameValues, []string{"github-username"}) || !valuesEqual(emailValues, []string{"42+github-username@users.noreply.github.com"}) {
		t.Fatalf("author = %q <%q>", nameValues, emailValues)
	}
	paths, err := git.Paths(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.HostsFile, paths.GitConfigFile, paths.StateFile} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if bytes.Contains(data, []byte("secret-token")) || bytes.Contains(data, []byte("oauth_token")) {
			t.Fatalf("secret persisted in %s: %q", path, data)
		}
	}

	application.Out = new(bytes.Buffer)
	if err := application.Run(context.Background(), []string{"credential", "--managed", "get"}, strings.NewReader("protocol=https\nhost=github.com\n\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(application.Out.(*bytes.Buffer).String(), "password=secret-token") {
		t.Fatal("credential helper did not return the token to Git")
	}

	if err := application.Run(context.Background(), []string{"unbind"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	nameValues, _ = git.LocalValues(context.Background(), root, "user.name")
	emailValues, _ = git.LocalValues(context.Background(), root, "user.email")
	if !valuesEqual(nameValues, []string{"prior name"}) || !valuesEqual(emailValues, []string{"prior@example.invalid"}) {
		t.Fatalf("restored author = %q <%q>", nameValues, emailValues)
	}
	if values, _ := git.LocalValues(context.Background(), root, gitconfig.IdentityKey); len(values) != 0 {
		t.Fatalf("identity remained after unbind: %#v", values)
	}
}

func TestParallelRepositoriesDoNotShareBinding(t *testing.T) {
	type result struct {
		account string
		name    string
		err     error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for _, item := range []struct {
		account string
		name    string
		id      int64
	}{
		{account: "github-username", name: "Test User", id: 42},
		{account: "github-alt-username", name: "Alternate User", id: 43},
	} {
		item := item
		wait.Add(1)
		go func() {
			defer wait.Done()
			git, root := newTestRepo(t, "https://github.com/example/repo.git", "before", "before@example.invalid")
			auth := fakeAuth{
				profiles: map[string]identity.Profile{"github.com\x00" + item.account: {ID: item.id, Login: item.account, Name: item.name}},
				tokens:   map[string]string{"github.com\x00" + item.account: "token-" + item.account},
			}
			application, _, _ := testApp(git, auth)
			err := application.Bind(context.Background(), item.account, "")
			if err != nil {
				results <- result{err: err}
				return
			}
			values, _ := git.LocalValues(context.Background(), root, gitconfig.IdentityKey)
			nameValues, _ := git.LocalValues(context.Background(), root, "user.name")
			results <- result{account: values[0], name: nameValues[0]}
		}()
	}
	wait.Wait()
	close(results)
	for got := range results {
		if got.err != nil {
			t.Fatal(got.err)
		}
		if (got.account != "github-username" && got.account != "github-alt-username") || got.name == "before" {
			t.Fatalf("parallel binding result = %+v", got)
		}
	}
}

func TestMalformedBindingIsRejected(t *testing.T) {
	git, root := newTestRepo(t, "", "name", "email@example.invalid")
	if err := git.ReplaceLocal(context.Background(), root, gitconfig.IdentityKey, []string{"../secret"}); err != nil {
		t.Fatal(err)
	}
	application, _, _ := testApp(git, fakeAuth{})
	if err := application.Status(context.Background(), false); err == nil {
		t.Fatal("status accepted malformed repository identity")
	}
}

func TestBindRejectsSymlinkedGeneratedDirectory(t *testing.T) {
	git, root := newTestRepo(t, "https://github.com/example/repo.git", "name", "email@example.invalid")
	paths, err := git.Paths(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.GeneratedDir); err != nil {
		t.Fatal(err)
	}
	auth := fakeAuth{
		profiles: map[string]identity.Profile{"github.com\x00github-username": {ID: 42, Login: "github-username"}},
		tokens:   map[string]string{"github.com\x00github-username": "secret-token"},
	}
	application, _, _ := testApp(git, auth)
	if err := application.Bind(context.Background(), "github-username", ""); err == nil {
		t.Fatal("bind followed a symlinked generated directory")
	}
}
