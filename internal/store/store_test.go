package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRepositoryNormalizesCommonForms(t *testing.T) {
	cases := map[string]Repository{
		"f4ah6o/example":                          {Host: "github.com", Owner: "f4ah6o", Name: "example"},
		"github.com/f4ah6o/example":               {Host: "github.com", Owner: "f4ah6o", Name: "example"},
		"https://github.com/f4ah6o/example.git":   {Host: "github.com", Owner: "f4ah6o", Name: "example"},
		"git@github.com:f4ah6o/example.git":       {Host: "github.com", Owner: "f4ah6o", Name: "example"},
		"ssh://git@github.com/f4ah6o/example.git": {Host: "github.com", Owner: "f4ah6o", Name: "example"},
	}
	for input, want := range cases {
		got, err := ParseRepository(input)
		if err != nil {
			t.Fatalf("ParseRepository(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseRepository(%q) = %+v, want %+v", input, got, want)
		}
	}
}

func TestParseRepositoryRejectsAmbiguousAndUnsafeInputs(t *testing.T) {
	for _, input := range []string{"example", "../example", "owner/../repo", "https://github.com/owner/../repo"} {
		if _, err := ParseRepository(input); err == nil {
			t.Fatalf("ParseRepository(%q) unexpectedly succeeded", input)
		}
	}
}

func TestManagerRemoteURLDefaultsToCanonicalURL(t *testing.T) {
	repo := Repository{Host: "github.com", Owner: "f4ah6o", Name: "example"}
	manager := New(t.TempDir())
	if got, want := manager.remoteURL(repo), "https://github.com/f4ah6o/example.git"; got != want {
		t.Fatalf("remote URL = %q, want %q", got, want)
	}
}

func TestRedactRemovesCredentials(t *testing.T) {
	input := "Authorization: Bearer secret-token\nhttps://user:password@github.com/example/repo.git\ntoken=another-secret"
	got := redact(input)
	for _, secret := range []string{"secret-token", "password@github.com", "another-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output leaked %q: %q", secret, got)
		}
	}
}

func TestStorePathCannotEscapeRoot(t *testing.T) {
	manager := New(t.TempDir())
	if _, err := manager.StorePath(Repository{Host: "github.com", Owner: "..", Name: "repo"}); err == nil {
		t.Fatal("unsafe repository identity unexpectedly produced a store path")
	}
}

func TestEnsureFetchAndFailedFetchPreserveLastObservation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	run(t, "", "git", "init", "--bare", "--quiet", origin)
	run(t, origin, "git", "symbolic-ref", "HEAD", "refs/heads/main")

	writer := filepath.Join(root, "writer")
	run(t, "", "git", "init", "--quiet", "--initial-branch=main", writer)
	run(t, writer, "git", "config", "user.name", "Test User")
	run(t, writer, "git", "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(writer, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, writer, "git", "add", "file.txt")
	run(t, writer, "git", "commit", "--quiet", "-m", "initial")
	run(t, writer, "git", "remote", "add", "origin", origin)
	run(t, writer, "git", "push", "--quiet", "origin", "main")

	repo := Repository{Host: "github.com", Owner: "example", Name: "repo"}
	manager := New(filepath.Join(root, "stores"))
	manager.RemoteURL = func(Repository) string { return origin }
	manager.Now = func() time.Time { return time.Date(2026, 9, 26, 1, 2, 3, 0, time.UTC) }

	result, err := manager.Ensure(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	storePath := result.Store.Path
	reused, err := manager.Ensure(ctx, repo)
	if err != nil {
		t.Fatalf("idempotent ensure: %v", err)
	}
	if reused.Store.Path != storePath {
		t.Fatalf("idempotent ensure changed store path: %q != %q", reused.Store.Path, storePath)
	}
	result, err = manager.Fetch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	first := result.Observations["refs/remotes/origin/main"]
	if first.Commit == "" || first.ObservedAt == "" {
		t.Fatalf("first observation = %+v", first)
	}
	if result.Remote.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", result.Remote.DefaultBranch)
	}
	if _, err := os.Stat(filepath.Join(storePath, "refs", "heads", "main")); !os.IsNotExist(err) {
		t.Fatalf("store unexpectedly created local refs/heads/main: err=%v", err)
	}

	if err := os.WriteFile(filepath.Join(writer, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, writer, "git", "add", "file.txt")
	run(t, writer, "git", "commit", "--quiet", "-m", "update")
	run(t, writer, "git", "push", "--quiet", "origin", "main")

	manager.Now = func() time.Time { return time.Date(2026, 9, 26, 1, 3, 0, 0, time.UTC) }
	result, err = manager.Fetch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	second := result.Observations["refs/remotes/origin/main"]
	if second.Commit == first.Commit {
		t.Fatal("remote-tracking observation did not advance")
	}
	successAt := result.Fetch.LastSuccessAt

	run(t, writer, "git", "push", "--quiet", "origin", "HEAD:topic")
	manager.Now = func() time.Time { return time.Date(2026, 9, 26, 1, 3, 30, 0, time.UTC) }
	result, err = manager.Fetch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Observations["refs/remotes/origin/topic"]; !ok {
		t.Fatal("new remote branch was not observed")
	}
	run(t, writer, "git", "push", "--quiet", "origin", "--delete", "topic")
	manager.Now = func() time.Time { return time.Date(2026, 9, 26, 1, 3, 45, 0, time.UTC) }
	result, err = manager.Fetch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Observations["refs/remotes/origin/topic"]; ok {
		t.Fatal("pruned remote branch remained in fresh observations")
	}
	successAt = result.Fetch.LastSuccessAt

	if err := os.Rename(origin, origin+".offline"); err != nil {
		t.Fatal(err)
	}
	manager.Now = func() time.Time { return time.Date(2026, 9, 26, 1, 4, 0, 0, time.UTC) }
	result, err = manager.Fetch(ctx, repo)
	if err == nil {
		t.Fatal("fetch against unavailable origin unexpectedly succeeded")
	}
	if result.Fetch.LastSuccessAt != successAt {
		t.Fatalf("last success changed after failed fetch: %q -> %q", successAt, result.Fetch.LastSuccessAt)
	}
	if result.Fetch.LastAttemptAt == result.Fetch.LastSuccessAt {
		t.Fatalf("failed attempt was reported as successful: %+v", result.Fetch)
	}
	if got := result.Observations["refs/remotes/origin/main"].Commit; got != second.Commit {
		t.Fatalf("last successful observation changed after failed fetch: %s != %s", got, second.Commit)
	}
	if result.Fetch.LastError == nil || result.Fetch.LastError.Code != "fetch_failed" {
		t.Fatalf("failed fetch did not produce structured error: %+v", result.Fetch.LastError)
	}
}

func TestEnsureRejectsPhysicalSymlinkEscape(t *testing.T) {
	for _, location := range []string{"host", "owner"} {
		location := location
		t.Run(location, func(t *testing.T) {
			storeRoot := filepath.Join(t.TempDir(), "stores")
			if err := os.MkdirAll(storeRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("untouched\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			repo := Repository{Host: "github.com", Owner: "example", Name: "repo"}
			linkPath := filepath.Join(storeRoot, repo.Host)
			if location == "owner" {
				if err := os.MkdirAll(linkPath, 0o700); err != nil {
					t.Fatal(err)
				}
				linkPath = filepath.Join(linkPath, repo.Owner)
			}
			if err := os.Symlink(external, linkPath); err != nil {
				t.Fatal(err)
			}

			_, err := New(storeRoot).Ensure(context.Background(), repo)
			if err == nil {
				t.Fatal("Ensure unexpectedly succeeded through an external symlink")
			}
			escaped, ok := err.(*Error)
			if !ok || escaped.Code != "path_escape" {
				t.Fatalf("Ensure error = %#v, want *Error code path_escape", err)
			}

			entries, err := os.ReadDir(external)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "sentinel" {
				t.Fatalf("external target was modified: %+v", entries)
			}
		})
	}
}

func TestFetchAtomicFailurePreservesLastSuccessAndObservations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	run(t, "", "git", "init", "--bare", "--quiet", origin)
	run(t, origin, "git", "symbolic-ref", "HEAD", "refs/heads/main")

	writer := filepath.Join(root, "writer")
	run(t, "", "git", "init", "--quiet", "--initial-branch=main", writer)
	run(t, writer, "git", "config", "user.name", "Test User")
	run(t, writer, "git", "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(writer, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, writer, "git", "add", "file.txt")
	run(t, writer, "git", "commit", "--quiet", "-m", "initial")
	run(t, writer, "git", "remote", "add", "origin", origin)
	run(t, writer, "git", "push", "--quiet", "origin", "main")
	run(t, writer, "git", "push", "--quiet", "origin", "HEAD:topic")

	repo := Repository{Host: "github.com", Owner: "example", Name: "repo"}
	manager := New(filepath.Join(root, "stores"))
	manager.RemoteURL = func(Repository) string { return origin }
	manager.Now = func() time.Time {
		return time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC)
	}

	ensured, err := manager.Ensure(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	storePath := ensured.Store.Path

	first, err := manager.Fetch(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	const mainRef = "refs/remotes/origin/main"
	const topicRef = "refs/remotes/origin/topic"
	firstMain, ok := first.Observations[mainRef]
	if !ok || firstMain.Commit == "" {
		t.Fatalf("initial main observation = %+v", first.Observations[mainRef])
	}
	firstTopic, ok := first.Observations[topicRef]
	if !ok || firstTopic.Commit == "" {
		t.Fatalf("initial topic observation = %+v", first.Observations[topicRef])
	}

	if err := os.WriteFile(filepath.Join(writer, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, writer, "git", "add", "file.txt")
	run(t, writer, "git", "commit", "--quiet", "-m", "update")
	run(t, writer, "git", "push", "--quiet", "origin", "main")
	run(t, writer, "git", "push", "--quiet", "origin", "--delete", "topic")

	lockPath := filepath.Join(storePath, "refs", "remotes", "origin", "topic.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("held\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager.Now = func() time.Time {
		return time.Date(2026, 9, 27, 1, 3, 0, 0, time.UTC)
	}
	failed, err := manager.Fetch(ctx, repo)
	if err == nil {
		t.Fatal("atomic fetch unexpectedly succeeded")
	}
	if failed.Fetch.LastSuccessAt != first.Fetch.LastSuccessAt {
		t.Fatalf("last success changed: %q -> %q", first.Fetch.LastSuccessAt, failed.Fetch.LastSuccessAt)
	}
	if failed.Fetch.LastAttemptAt == first.Fetch.LastAttemptAt {
		t.Fatalf("failed attempt timestamp did not advance: %+v", failed.Fetch)
	}
	if failed.Fetch.LastError == nil || failed.Fetch.LastError.Code != "fetch_failed" {
		t.Fatalf("failed fetch error = %+v", failed.Fetch.LastError)
	}

	mainAfter, ok := failed.Observations[mainRef]
	if !ok || mainAfter.Commit != firstMain.Commit || mainAfter.ObservedAt != firstMain.ObservedAt {
		t.Fatalf("main observation changed after atomic failure: before=%+v after=%+v", firstMain, mainAfter)
	}
	topicAfter, ok := failed.Observations[topicRef]
	if !ok || topicAfter.Commit != firstTopic.Commit || topicAfter.ObservedAt != firstTopic.ObservedAt {
		t.Fatalf("topic observation was pruned or changed after atomic failure: before=%+v after=%+v", firstTopic, topicAfter)
	}
	if got := strings.TrimSpace(run(t, storePath, "git", "rev-parse", mainRef)); got != firstMain.Commit {
		t.Fatalf("local main ref changed: %s != %s", got, firstMain.Commit)
	}
	if got := strings.TrimSpace(run(t, storePath, "git", "rev-parse", topicRef)); got != firstTopic.Commit {
		t.Fatalf("local topic ref changed: %s != %s", got, firstTopic.Commit)
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
