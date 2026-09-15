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

func TestBindCredentialAndUnbind(t *testing.T) {
	git, root := newTestRepo(t, "https://github.com/example/gh-git.git", "prior name", "prior@example.invalid")
	auth := fakeAuth{
		profiles: map[string]identity.Profile{"github.com\x00github-username": {ID: 42, Login: "github-username", Name: "Test User"}},
		tokens:   map[string]string{"github.com\x00github-username": "secret-token"},
	}
	application, out, _ := testApp(git, auth)
	if err := application.Bind(context.Background(), "github-username", ""); err != nil {
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
	if err := application.Credential(context.Background(), "get", strings.NewReader("protocol=https\nhost=github.com\n\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(application.Out.(*bytes.Buffer).String(), "password=secret-token") {
		t.Fatal("credential helper did not return the token to Git")
	}

	if err := application.Unbind(context.Background()); err != nil {
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
