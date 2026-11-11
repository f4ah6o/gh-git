package app

import (
	"bytes"
	"context"
	"errors"
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

type cloneAuth struct {
	accounts      []ghauth.Account
	tokens        map[string]string
	profiles      map[string]identity.Profile
	profileErrors map[string]error

	mu            sync.Mutex
	tokenCalls    []string
	profileCalls  []string
	accountsCalls int
}

func cloneAuthKey(host, account string) string { return host + "\x00" + account }

func (c *cloneAuth) Token(_ context.Context, host, account string) (string, error) {
	c.mu.Lock()
	c.tokenCalls = append(c.tokenCalls, cloneAuthKey(host, account))
	c.mu.Unlock()
	if token := c.tokens[cloneAuthKey(host, account)]; token != "" {
		return token, nil
	}
	return "", os.ErrNotExist
}

func (c *cloneAuth) Profile(_ context.Context, host, account string) (identity.Profile, error) {
	c.mu.Lock()
	c.profileCalls = append(c.profileCalls, cloneAuthKey(host, account))
	c.mu.Unlock()
	if err := c.profileErrors[cloneAuthKey(host, account)]; err != nil {
		return identity.Profile{}, err
	}
	if profile, ok := c.profiles[cloneAuthKey(host, account)]; ok {
		return profile, nil
	}
	return identity.Profile{}, os.ErrNotExist
}

func (c *cloneAuth) Accounts(_ context.Context, _ string) ([]ghauth.Account, error) {
	c.mu.Lock()
	c.accountsCalls++
	c.mu.Unlock()
	result := make([]ghauth.Account, len(c.accounts))
	copy(result, c.accounts)
	return result, nil
}

func (c *cloneAuth) calls() (tokens, profiles []string, accounts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.tokenCalls...), append([]string{}, c.profileCalls...), c.accountsCalls
}

func cloneAuthFor(login string) *cloneAuth {
	return &cloneAuth{
		accounts: []ghauth.Account{{Host: "github.com", Login: login, Active: true, State: "success"}},
		tokens:   map[string]string{cloneAuthKey("github.com", login): "token-" + login},
		profiles: map[string]identity.Profile{cloneAuthKey("github.com", login): {ID: 42, Login: login}},
	}
}

func runGitForTest(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Env = env
	if dir != "" {
		command.Dir = dir
	}
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newCloneFixture creates an isolated bare remote and rewrites the GitHub URL
// to it, so clone tests exercise real Git without network access.
func newCloneFixture(t *testing.T, owner, repo string) (*gitconfig.Client, string, string) {
	t.Helper()
	tmp := t.TempDir()
	env := append([]string{}, os.Environ()...)
	env = append(env, "HOME="+tmp, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(tmp, "global.gitconfig"))

	remote := filepath.Join(tmp, "remote.git")
	runGitForTest(t, env, "", "init", "--bare", "-q", remote)
	seed := filepath.Join(tmp, "seed")
	runGitForTest(t, env, "", "init", "-q", seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, env, seed, "add", "README.md")
	runGitForTest(t, env, seed, "-c", "user.name=Seed", "-c", "user.email=seed@example.invalid", "commit", "-m", "initial")
	runGitForTest(t, env, seed, "push", remote, "HEAD:main")
	runGitForTest(t, env, "", "--git-dir="+remote, "symbolic-ref", "HEAD", "refs/heads/main")

	globalConfig := filepath.Join(tmp, "global.gitconfig")
	runGitForTest(t, env, "", "config", "--file", globalConfig,
		"url.file://"+filepath.ToSlash(remote)+".insteadOf",
		"https://github.com/"+owner+"/"+repo+".git")

	git := gitconfig.New()
	git.WorkDir = tmp
	git.Env = env
	return git, tmp, remote
}

func assertBoundTo(t *testing.T, git *gitconfig.Client, root, account, email string) {
	t.Helper()
	ctx := context.Background()
	identities, err := git.LocalValues(ctx, root, gitconfig.IdentityKey)
	if err != nil || len(identities) != 1 || identities[0] != account {
		t.Fatalf("identity = %#v, err = %v", identities, err)
	}
	names, _ := git.LocalValues(ctx, root, "user.name")
	emails, _ := git.LocalValues(ctx, root, "user.email")
	if !valuesEqual(names, []string{account}) {
		t.Fatalf("user.name = %#v", names)
	}
	if !valuesEqual(emails, []string{email}) {
		t.Fatalf("user.email = %#v", emails)
	}
	remotes, err := git.Remotes(ctx, root)
	if err != nil || len(remotes) != 1 || remotes[0].Host != "github.com" || !remotes[0].HTTPS() {
		t.Fatalf("remotes = %#v, err = %v", remotes, err)
	}
	paths, err := git.Paths(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.StateFile); err != nil {
		t.Fatalf("state file: %v", err)
	}
}

func TestCloneBindsOwnerAccount(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	auth := &cloneAuth{
		accounts: []ghauth.Account{
			{Host: "github.com", Login: "f4ah6o", Active: true, State: "success"},
			{Host: "github.com", Login: "other-account", Active: false, State: "success"},
		},
		tokens: map[string]string{
			cloneAuthKey("github.com", "f4ah6o"):        "token-owner",
			cloneAuthKey("github.com", "other-account"): "token-other",
		},
		profiles: map[string]identity.Profile{
			cloneAuthKey("github.com", "f4ah6o"): {ID: 42, Login: "f4ah6o"},
		},
	}
	application, out, errOut := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	destination := filepath.Join(workDir, "gh-git")
	assertBoundTo(t, git, destination, "f4ah6o", "42+f4ah6o@users.noreply.github.com")
	if !strings.Contains(out.String(), "Cloned f4ah6o/gh-git into "+destination) {
		t.Fatalf("clone output = %q", out.String())
	}
	if !strings.Contains(out.String(), "Bound repository to github.com account f4ah6o.") {
		t.Fatalf("bind output missing: %q", out.String())
	}
	for _, token := range []string{"token-owner", "token-other"} {
		if strings.Contains(out.String(), token) || strings.Contains(errOut.String(), token) {
			t.Fatalf("output leaked %q", token)
		}
	}
	_, profiles, _ := auth.calls()
	if len(profiles) != 1 || profiles[0] != cloneAuthKey("github.com", "f4ah6o") {
		t.Fatalf("profile calls = %#v", profiles)
	}
}

func TestCloneExplicitAccountOverridesOwner(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	auth := &cloneAuth{
		accounts: []ghauth.Account{
			{Host: "github.com", Login: "f4ah6o", Active: true, State: "success"},
			{Host: "github.com", Login: "work-account", Active: false, State: "success"},
		},
		tokens: map[string]string{
			cloneAuthKey("github.com", "f4ah6o"):       "token-owner",
			cloneAuthKey("github.com", "work-account"): "token-work",
		},
		profiles: map[string]identity.Profile{
			cloneAuthKey("github.com", "work-account"): {ID: 7, Login: "work-account"},
		},
	}
	application, out, errOut := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git", "--account", "work-account"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	destination := filepath.Join(workDir, "gh-git")
	assertBoundTo(t, git, destination, "work-account", "7+work-account@users.noreply.github.com")
	if !strings.Contains(out.String(), "as work-account") {
		t.Fatalf("clone output = %q", out.String())
	}
	_, profiles, _ := auth.calls()
	if len(profiles) != 1 || profiles[0] != cloneAuthKey("github.com", "work-account") {
		t.Fatalf("profile calls = %#v", profiles)
	}
}

func TestCloneOrganizationOwnerUsesOnlyUsableAccount(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "example-org", "gh-git")
	auth := cloneAuthFor("f4ah6o")
	application, _, errOut := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"clone", "example-org/gh-git"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	assertBoundTo(t, git, filepath.Join(workDir, "gh-git"), "f4ah6o", "42+f4ah6o@users.noreply.github.com")
}

func TestCloneAmbiguousAccountsRequireExplicitAccount(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "example-org", "gh-git")
	auth := &cloneAuth{
		accounts: []ghauth.Account{
			{Host: "github.com", Login: "alpha", Active: true, State: "success"},
			{Host: "github.com", Login: "beta", Active: false, State: "success"},
		},
		tokens: map[string]string{
			cloneAuthKey("github.com", "alpha"): "token-alpha",
			cloneAuthKey("github.com", "beta"):  "token-beta",
		},
	}
	application, _, _ := testApp(git, auth)
	err := application.Run(context.Background(), []string{"clone", "example-org/gh-git"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("clone unexpectedly chose an account")
	}
	for _, want := range []string{"multiple accounts are eligible", "alpha", "beta", "--account"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(workDir, "gh-git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination was created: %v", statErr)
	}
	_, profiles, _ := auth.calls()
	if len(profiles) != 0 {
		t.Fatalf("bind ran despite ambiguous accounts: %#v", profiles)
	}
}

func TestCloneCustomDestination(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, errOut := testApp(git, cloneAuthFor("f4ah6o"))
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git", "custom-dir"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	assertBoundTo(t, git, filepath.Join(workDir, "custom-dir"), "f4ah6o", "42+f4ah6o@users.noreply.github.com")
}

func TestCloneFailureDoesNotBind(t *testing.T) {
	git, workDir, remote := newCloneFixture(t, "f4ah6o", "gh-git")
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	auth := cloneAuthFor("f4ah6o")
	application, _, errOut := testApp(git, auth)
	err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("clone unexpectedly succeeded")
	}
	if code, ok := ExitCode(err); !ok || code == 0 {
		t.Fatalf("clone exit = (%d, %v), err = %v", code, ok, err)
	}
	if errOut.Len() == 0 {
		t.Fatal("git clone stderr was not forwarded")
	}
	if _, statErr := os.Stat(filepath.Join(workDir, "gh-git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed clone left destination: %v", statErr)
	}
	_, profiles, _ := auth.calls()
	if len(profiles) != 0 {
		t.Fatalf("bind ran after clone failure: %#v", profiles)
	}
}

func TestCloneBindFailureKeepsRepositoryAndPrintsRecovery(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	auth := cloneAuthFor("f4ah6o")
	auth.profileErrors = map[string]error{cloneAuthKey("github.com", "f4ah6o"): errors.New("profile unavailable")}
	application, out, errOut := testApp(git, auth)
	err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader(""))
	if err == nil {
		t.Fatal("clone unexpectedly succeeded")
	}
	var bindErr *CloneBindError
	if !errors.As(err, &bindErr) {
		t.Fatalf("error = %T %v, want *CloneBindError", err, err)
	}
	destination := filepath.Join(workDir, "gh-git")
	if _, statErr := os.Stat(filepath.Join(destination, ".git")); statErr != nil {
		t.Fatalf("clone was not kept: %v", statErr)
	}
	if !strings.Contains(out.String(), "Cloned f4ah6o/gh-git into "+destination) {
		t.Fatalf("stdout = %q", out.String())
	}
	for _, want := range []string{"The clone was kept", "gh git bind", destination, "f4ah6o"} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want %q", errOut.String(), want)
		}
	}
	paths, pathErr := git.Paths(context.Background(), destination)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Stat(paths.StateFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("state file exists after bind failure: %v", statErr)
	}
}

func TestCloneRefusesExistingDestination(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	destination := filepath.Join(workDir, "gh-git")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	auth := cloneAuthFor("f4ah6o")
	application, _, _ := testApp(git, auth)
	err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want existing destination error", err)
	}
	data, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(data) != "keep\n" {
		t.Fatalf("sentinel = %q, err = %v", data, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(destination, ".git")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("existing destination was modified: %v", statErr)
	}
	if _, _, accounts := auth.calls(); accounts != 0 {
		t.Fatalf("account resolution ran before destination check: %d", accounts)
	}
}

func TestCloneDestinationWithSpaces(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, errOut := testApp(git, cloneAuthFor("f4ah6o"))
	parent := filepath.Join(workDir, "dir with spaces")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join("dir with spaces", "my repo")
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git", relative}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	assertBoundTo(t, git, filepath.Join(workDir, relative), "f4ah6o", "42+f4ah6o@users.noreply.github.com")
}

func TestCloneAbsoluteDestinationWithSpaces(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, errOut := testApp(git, cloneAuthFor("f4ah6o"))
	parent := filepath.Join(workDir, "abs dir")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "repo with space")
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git", destination}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	assertBoundTo(t, git, destination, "f4ah6o", "42+f4ah6o@users.noreply.github.com")
}

func TestCloneDoesNotChangeWorkingDirectory(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	git, _, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, errOut := testApp(git, cloneAuthFor("f4ah6o"))
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("working directory changed from %q to %q", before, after)
	}
}

func TestCloneDoesNotMutateGlobalAuthenticationState(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	globalConfig := filepath.Join(workDir, "global.gitconfig")
	before, err := os.ReadFile(globalConfig)
	if err != nil {
		t.Fatal(err)
	}
	envBefore := append([]string{}, git.Env...)
	auth := &cloneAuth{
		accounts: []ghauth.Account{
			{Host: "github.com", Login: "global-account", Active: true, State: "success"},
			{Host: "github.com", Login: "selected-account", Active: false, State: "success"},
		},
		tokens: map[string]string{
			cloneAuthKey("github.com", "global-account"):   "token-global",
			cloneAuthKey("github.com", "selected-account"): "token-selected",
		},
		profiles: map[string]identity.Profile{
			cloneAuthKey("github.com", "selected-account"): {ID: 9, Login: "selected-account"},
		},
	}
	application, _, errOut := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git", "--account", "selected-account"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	after, err := os.ReadFile(globalConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("global git config changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !valuesEqual(envBefore, git.Env) {
		t.Fatalf("git environment changed:\nbefore: %#v\nafter: %#v", envBefore, git.Env)
	}
	_, profiles, _ := auth.calls()
	if len(profiles) != 1 || profiles[0] != cloneAuthKey("github.com", "selected-account") {
		t.Fatalf("profile calls = %#v", profiles)
	}
}

func TestCloneDoesNotDiscloseTokens(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	auth := cloneAuthFor("f4ah6o")
	auth.tokens[cloneAuthKey("github.com", "f4ah6o")] = "super-secret-token-value"
	application, out, errOut := testApp(git, auth)
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	for _, text := range []string{out.String(), errOut.String()} {
		if strings.Contains(text, "super-secret-token-value") {
			t.Fatalf("output leaked token: %q", text)
		}
	}
	destination := filepath.Join(workDir, "gh-git")
	err := filepath.Walk(filepath.Join(destination, ".git"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte("super-secret-token-value")) {
			t.Fatalf("token persisted in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	env := cloneGitEnv([]string{"GH_TOKEN=super-secret-token-value", "PATH=/bin"}, "github.com", "f4ah6o")
	for _, entry := range env {
		if strings.Contains(entry, "super-secret-token-value") {
			t.Fatalf("clone environment contains token: %q", entry)
		}
	}
}

func TestGitPassthroughCloneRemainsNative(t *testing.T) {
	git, workDir, remote := newCloneFixture(t, "f4ah6o", "gh-git")
	auth := cloneAuthFor("f4ah6o")
	application, _, errOut := testApp(git, auth)
	destination := filepath.Join(workDir, "passthrough")
	if err := application.Run(context.Background(), []string{"--", "clone", "file://" + filepath.ToSlash(remote), destination}, strings.NewReader("")); err != nil {
		t.Fatalf("passthrough clone: %v\nstderr: %s", err, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); err != nil {
		t.Fatalf("passthrough clone did not create repository: %v", err)
	}
	if values, _ := git.LocalValues(context.Background(), destination, gitconfig.IdentityKey); len(values) != 0 {
		t.Fatalf("passthrough clone was bound: %#v", values)
	}
	if _, _, accounts := auth.calls(); accounts != 0 {
		t.Fatalf("passthrough clone consulted gh-git auth: %d", accounts)
	}
}

func TestCloneRejectsArbitraryURLWithPassthroughHint(t *testing.T) {
	git, _, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, _ := testApp(git, cloneAuthFor("f4ah6o"))
	err := application.Run(context.Background(), []string{"clone", "https://github.com/f4ah6o/gh-git.git"}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "gh git -- clone") {
		t.Fatalf("error = %v, want passthrough hint", err)
	}
}

func TestCloneAcceptsGitSuffix(t *testing.T) {
	git, workDir, _ := newCloneFixture(t, "f4ah6o", "gh-git")
	application, _, errOut := testApp(git, cloneAuthFor("f4ah6o"))
	if err := application.Run(context.Background(), []string{"clone", "f4ah6o/gh-git.git"}, strings.NewReader("")); err != nil {
		t.Fatalf("clone: %v\nstderr: %s", err, errOut.String())
	}
	assertBoundTo(t, git, filepath.Join(workDir, "gh-git"), "f4ah6o", "42+f4ah6o@users.noreply.github.com")
}

func TestCloneCredentialHelperUsesSelectedAccount(t *testing.T) {
	auth := cloneAuthFor("f4ah6o")
	auth.tokens[cloneAuthKey("github.com", "f4ah6o")] = "secret-token"
	application, out, _ := testApp(gitconfig.New(), auth)
	t.Setenv(cloneHostEnv, "github.com")
	t.Setenv(cloneAccountEnv, "f4ah6o")
	input := "protocol=https\nhost=github.com\n\n"
	if err := application.Run(context.Background(), []string{"credential", "--clone", "get"}, strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	want := "protocol=https\nhost=github.com\nusername=f4ah6o\npassword=secret-token\n\n"
	if out.String() != want {
		t.Fatalf("helper output = %q, want %q", out.String(), want)
	}
}

func TestCloneCredentialHelperIgnoresOtherHosts(t *testing.T) {
	auth := cloneAuthFor("f4ah6o")
	application, out, _ := testApp(gitconfig.New(), auth)
	t.Setenv(cloneHostEnv, "github.com")
	t.Setenv(cloneAccountEnv, "f4ah6o")
	input := "protocol=https\nhost=example.invalid\n\n"
	if err := application.Run(context.Background(), []string{"credential", "--clone", "get"}, strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("helper returned credentials for another host: %q", out.String())
	}
}

func TestCloneCredentialHelperRequiresEnvironment(t *testing.T) {
	application, _, _ := testApp(gitconfig.New(), cloneAuthFor("f4ah6o"))
	t.Setenv(cloneHostEnv, "")
	t.Setenv(cloneAccountEnv, "")
	err := application.Run(context.Background(), []string{"credential", "--clone", "get"}, strings.NewReader("protocol=https\nhost=github.com\n\n"))
	if err == nil {
		t.Fatal("helper accepted missing environment")
	}
}

func TestCloneCredentialHelperDoesNotPersist(t *testing.T) {
	application, out, _ := testApp(gitconfig.New(), cloneAuthFor("f4ah6o"))
	t.Setenv(cloneHostEnv, "github.com")
	t.Setenv(cloneAccountEnv, "f4ah6o")
	for _, operation := range []string{"store", "erase"} {
		out.Reset()
		input := "protocol=https\nhost=github.com\nusername=f4ah6o\npassword=secret\n\n"
		if err := application.Run(context.Background(), []string{"credential", "--clone", operation}, strings.NewReader(input)); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("%s wrote output: %q", operation, out.String())
		}
	}
}

func TestCloneGitArgsAndEnv(t *testing.T) {
	args := cloneGitArgs("https://github.com/f4ah6o/gh-git.git", "/tmp/dest")
	want := []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=!gh git credential --clone",
		"clone", "--", "https://github.com/f4ah6o/gh-git.git", "/tmp/dest",
	}
	if !valuesEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	env := cloneGitEnv([]string{
		"PATH=/bin",
		"GH_TOKEN=ambient",
		"GITHUB_TOKEN=ambient",
		"GH_ENTERPRISE_TOKEN=ambient",
		"GITHUB_ENTERPRISE_TOKEN=ambient",
		"GIT_ASKPASS=/tmp/askpass",
		"SSH_ASKPASS=/tmp/askpass",
		"GIT_TERMINAL_PROMPT=1",
		"GIT_TRACE_REDACT=0",
	}, "github.com", "f4ah6o")
	joined := strings.Join(env, "\n")
	for _, unwanted := range []string{
		"GH_TOKEN=", "GITHUB_TOKEN=", "GH_ENTERPRISE_TOKEN=", "GITHUB_ENTERPRISE_TOKEN=",
		"GIT_ASKPASS=", "SSH_ASKPASS=", "GIT_TERMINAL_PROMPT=1", "GIT_TRACE_REDACT=",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("environment contains %q: %q", unwanted, joined)
		}
	}
	for _, wanted := range []string{"PATH=/bin", cloneHostEnv + "=github.com", cloneAccountEnv + "=f4ah6o", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("environment missing %q: %q", wanted, joined)
		}
	}
}

func TestParseCloneArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    cloneOptions
		wantErr string
	}{
		{
			name: "owner repo",
			args: []string{"f4ah6o/gh-git"},
			want: cloneOptions{owner: "f4ah6o", repo: "gh-git", host: "github.com"},
		},
		{
			name: "destination and account",
			args: []string{"f4ah6o/gh-git", "dir with spaces", "--account", "work"},
			want: cloneOptions{owner: "f4ah6o", repo: "gh-git", host: "github.com", account: "work", destination: "dir with spaces"},
		},
		{
			name: "equals flags",
			args: []string{"--account=work", "--hostname=github.example.com", "f4ah6o/gh-git"},
			want: cloneOptions{owner: "f4ah6o", repo: "gh-git", host: "github.example.com", account: "work"},
		},
		{
			name: "separator",
			args: []string{"--", "f4ah6o/gh-git", "-destination"},
			want: cloneOptions{owner: "f4ah6o", repo: "gh-git", host: "github.com", destination: "-destination"},
		},
		{
			name: "git suffix",
			args: []string{"f4ah6o/gh-git.git"},
			want: cloneOptions{owner: "f4ah6o", repo: "gh-git", host: "github.com"},
		},
		{
			name:    "url rejected",
			args:    []string{"https://github.com/f4ah6o/gh-git.git"},
			wantErr: "gh git -- clone",
		},
		{
			name:    "missing spec",
			args:    nil,
			wantErr: "usage",
		},
		{
			name:    "too many positional",
			args:    []string{"f4ah6o/gh-git", "one", "two"},
			wantErr: "usage",
		},
		{
			name:    "unknown flag",
			args:    []string{"--force", "f4ah6o/gh-git"},
			wantErr: "unknown clone flag",
		},
		{
			name:    "missing account value",
			args:    []string{"f4ah6o/gh-git", "--account"},
			wantErr: "--account requires a value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCloneArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("options = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResolveCloneAccount(t *testing.T) {
	cases := []struct {
		name      string
		accounts  []ghauth.Account
		tokens    map[string]string
		owner     string
		requested string
		want      string
		wantErr   string
	}{
		{
			name: "owner is authenticated",
			accounts: []ghauth.Account{
				{Login: "owner", State: "success"},
				{Login: "other", State: "success"},
			},
			tokens: map[string]string{cloneAuthKey("github.com", "owner"): "t", cloneAuthKey("github.com", "other"): "t"},
			owner:  "owner",
			want:   "owner",
		},
		{
			name:     "organization with one account",
			accounts: []ghauth.Account{{Login: "solo", State: "success"}},
			tokens:   map[string]string{cloneAuthKey("github.com", "solo"): "t"},
			owner:    "org",
			want:     "solo",
		},
		{
			name: "ambiguous",
			accounts: []ghauth.Account{
				{Login: "alpha", State: "success"},
				{Login: "beta", State: "success"},
			},
			tokens:  map[string]string{cloneAuthKey("github.com", "alpha"): "t", cloneAuthKey("github.com", "beta"): "t"},
			owner:   "org",
			wantErr: "multiple accounts are eligible",
		},
		{
			name: "explicit account",
			accounts: []ghauth.Account{
				{Login: "owner", State: "success"},
				{Login: "work", State: "success"},
			},
			tokens:    map[string]string{cloneAuthKey("github.com", "owner"): "t", cloneAuthKey("github.com", "work"): "t"},
			owner:     "owner",
			requested: "work",
			want:      "work",
		},
		{
			name:      "explicit account not authenticated",
			accounts:  []ghauth.Account{{Login: "owner", State: "success"}},
			tokens:    map[string]string{cloneAuthKey("github.com", "owner"): "t"},
			owner:     "owner",
			requested: "missing",
			wantErr:   "not an authenticated",
		},
		{
			name: "owner unusable falls back",
			accounts: []ghauth.Account{
				{Login: "owner", State: "success"},
				{Login: "other", State: "success"},
			},
			tokens: map[string]string{cloneAuthKey("github.com", "other"): "t"},
			owner:  "owner",
			want:   "other",
		},
		{
			name:     "owner unusable and alone",
			accounts: []ghauth.Account{{Login: "owner", State: "success"}},
			tokens:   map[string]string{},
			owner:    "owner",
			wantErr:  "no usable credential",
		},
		{
			name: "error state is ignored",
			accounts: []ghauth.Account{
				{Login: "owner", State: "error"},
				{Login: "other", State: "success"},
			},
			tokens: map[string]string{cloneAuthKey("github.com", "owner"): "t", cloneAuthKey("github.com", "other"): "t"},
			owner:  "owner",
			want:   "other",
		},
		{
			name:     "no accounts",
			accounts: nil,
			owner:    "owner",
			wantErr:  "no authenticated GitHub account",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth := &cloneAuth{accounts: tc.accounts, tokens: tc.tokens}
			application, _, _ := testApp(gitconfig.New(), auth)
			got, err := application.resolveCloneAccount(context.Background(), "github.com", tc.owner, tc.requested)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("account = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCloneHelpDocumentsAccountSelection(t *testing.T) {
	application, out, _ := testApp(gitconfig.New(), cloneAuthFor("f4ah6o"))
	if err := application.Run(context.Background(), []string{"clone", "--help"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gh git clone <owner/repo>",
		"--account <login>",
		"gh git -- clone",
		"never overwritten",
		"recovery command",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("clone help missing %q:\n%s", want, out.String())
		}
	}
}
