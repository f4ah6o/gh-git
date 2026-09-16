# gh-git

`gh-git` runs ordinary Git commands as `gh git ...` and can bind a repository
to one GitHub account. The binding controls the Git author and GitHub
authentication without changing the global active account used by
`gh auth switch`.

## Why this exists

GitHub CLI supports multiple accounts on one host, but its active account is
still host-scoped. `gh auth switch` changes the account used by ordinary `gh`
API commands and by `gh auth git-credential`; that shared state is unsafe when
two repositories, terminals, or agents run concurrently. GitHub CLI's own
[multiple-account notes](https://github.com/cli/cli/blob/trunk/docs/multiple-accounts.md)
also document automatic selection from the current directory as an unsupported
use case.

`gh-git` stores only this non-secret repository-local binding:

```ini
[github]
    identity = <github-username>
    host = github.com
```

It also sets repository-local `user.name` and `user.email`. On GitHub.com the
email is derived as `<numeric-id>+<login>@users.noreply.github.com`.

## Install

From a published extension repository:

```bash
gh extension install f4ah6o/gh-git
```

The remote install uses precompiled binaries attached to the latest GitHub
CalVer Release. A source-only repository cannot be installed by `gh extension install`;
run `.github/workflows/release.yml` to allocate a CalVer tag and publish the
platform-specific assets first. Releases use `YYYY.MM.PATCH` in the
`Asia/Tokyo` timezone, for example `2026.9.0`.

```bash
git tag -f latest
git push -f origin latest
```

The workflow can also be started from the GitHub Actions UI with
`workflow_dispatch`.

When developing locally:

```bash
go build -o gh-git .
# Run ./gh-git while developing, or install the pushed OWNER/REPO normally.
```

The extension uses only the Go standard library.

## Quick start

Source the shell integration once per shell. This is needed only for direct
`gh` commands; Git HTTPS authentication works from the repository binding
without the hook.

```bash
eval "$(gh git shell-init bash)"
cd /path/to/repository
gh git bind <github-username>
gh git binding status
```

After that, Git can be run through `gh git`; network operations use the
repository binding where authentication is needed, while direct `gh` commands
use the shell-selected profile:

```bash
gh git status
gh git fetch
gh git pull --ff-only
gh git add -- path/to/file
gh git commit -m "message"
gh git push
gh pr create
gh pr view
gh issue create
gh release create
gh api user
```

Use `zsh` or `fish` in place of `bash` where appropriate. The hook changes
`GH_CONFIG_DIR` only while the shell is inside a bound repository and restores
the previous value when it leaves. Because GitHub CLI gives `GH_TOKEN` and
`GITHUB_TOKEN` precedence over stored credentials, the hook temporarily
unsets the four GitHub token environment variables inside a bound repository
and restores them on exit; it never writes their values to the repository.

## Commands

Print the gh-git extension version with:

```bash
gh git --version
```

`gh git version` remains a normal Git passthrough and therefore runs
`git version`. Use `gh git -- --version` when you explicitly want Git's
global `--version` flag.

Normal Git commands are forwarded to the real `git` executable without a
shell. Arguments, standard input/output/error, current working directory,
environment, and Git's exit status are preserved.

```bash
gh git status
gh git fetch --prune
gh git pull --ff-only
gh git add -- path/to/file
gh git commit -m "message"
gh git push
gh git diff --stat
gh git log --oneline -10
gh git show HEAD
gh git branch -vv
gh git switch -c feature/example
gh git checkout -- path/to/file
gh git merge topic
gh git rebase main
gh git tag --list
gh git worktree list
gh git remote -v
gh git rev-parse HEAD
gh git ls-files
gh git grep pattern
gh git restore path/to/file
gh git reset HEAD -- path/to/file
```

Passthrough is generic rather than allow-listed. `gh git <arguments...>` has
the same destructive capabilities as `git <arguments...>`; gh-git does not
add confirmation, remove flags, or reinterpret Git semantics. Use an explicit
separator when command routing needs to be unambiguous:

```bash
gh git -- status --short
```

Repository-binding management is available under the `binding` namespace:

```bash
gh git bind <account> [--hostname <host>]
gh git unbind
gh git binding status
gh git binding status --json
gh git accounts
gh git doctor
gh git env
gh git shell-init bash
```

`bind` validates the requested account with `gh auth token --user` and reads
its `/user` metadata through `gh api`; it never calls `gh auth switch`.

`gh git binding status` reports the binding, author, Git wiring, profile path,
and whether the stored credential is available. It never prints a token.
`accounts` reports account names and status from `gh auth status`, without the
`--show-token` option.

For compatibility, `bind`, `unbind`, `accounts`, `doctor`, `env`, and
`shell-init` remain available as top-level gh-git commands. `status` and
`init` intentionally belong to Git itself, so use `gh git binding status` for
gh-git's diagnostic status.

`unbind` removes gh-git's generated wiring and restores the local author
values that existed before the first bind. If a user changed a managed value
after binding, it is left untouched and a warning is printed.

## Authentication design

### GitHub CLI

`gh-git` creates `.git/gh-git/gh-config/hosts.yml` containing only the bound
host and account name. It contains no `oauth_token`. The shell hook selects
that directory as `GH_CONFIG_DIR`. GitHub CLI then resolves the selected
account's token from its normal secure credential store.

The profile is deliberately separate per repository. It avoids the global
active-account pointer while preserving normal commands such as `gh pr create`
and `gh api`.

This tokenless profile depends on GitHub CLI being able to resolve the
per-user credential from its OS keyring. If `gh auth status` reports
`GH_CONFIG_DIR/hosts.yml` as the token source, `gh-git` will not copy that
secret into the repository profile: the Git helper can still use the original
configuration, but transparent direct `gh` commands need a secure keyring or
an explicitly managed process environment such as CI's `GH_TOKEN`.

Without the shell hook, an extension cannot intercept an unrelated invocation
such as `gh pr create`; it runs as a separate `gh git` process. In that case,
ordinary `gh` commands continue to use the normal global configuration. A
one-shot alternative for an agent is:

```bash
eval "$(gh git env --shell bash)"
gh pr create
```

This sets only the profile location, not a token. CI should use its normal
short-lived `GH_TOKEN`/`GITHUB_TOKEN` mechanism instead of committing or
creating a repository token file. See the official
[`GH_CONFIG_DIR` and token environment documentation](https://cli.github.com/manual/gh_help_environment).

### Git over HTTPS

The binding adds a repository-local include at `gh-git/git-config`. It clears
inherited credential helpers for this repository and installs:

```text
gh git credential --managed
```

When Git asks for credentials, the helper reads the binding and invokes
`gh auth token --hostname <host> --user <account>`. The token is held in
memory and returned only through Git's credential-helper protocol. It is not a
command-line argument, written to the repository, printed in diagnostics, or
stored by `store`/`erase` operations.

`credential.useHttpPath=true` prevents credentials from being accidentally
shared across unrelated GitHub paths by Git's own credential cache.

### Git over SSH

`gh-git` does not generate keys, upload keys, modify `~/.ssh`, or change the
configured remote URL. If an existing exact alias named `github-<github-username>` points to the
bound host, for example:

```sshconfig
Host github-<github-username>
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519_<github-username>
    IdentitiesOnly yes
```

`bind` adds a repository-local Git URL rewrite, leaving the visible remote as
`git@github.com:owner/repo.git` while SSH connects through the existing alias.
If the alias is absent, `binding status`/`doctor` reports that canonical SSH cannot
select an account per repository; add and verify the alias yourself, then
bind again. This avoids silently using the wrong SSH key.

## Security model

- Tokens remain in GitHub CLI's existing OS credential store.
- No token, PAT, OAuth credential, or password is written to `.git/config`,
  the generated profile, or a tracked file.
- Account and host values from repository config are validated before use.
- Generated files are confined to the repository's Git directory and are
  overwritten only when their gh-git marker is present.
- Errors and status output intentionally omit credential values.
- `gh auth switch` is never called, so binding repository A does not mutate
  repository B's or another terminal's active account.
- A repository config is untrusted input. Do not accept a binding or generated
  file from an untrusted worktree without reviewing it.

## Parallel repositories and agents

For example:

```text
repo-a/.git/config -> github.identity=<github-username>
repo-b/.git/config -> github.identity=<github-username>
```

Each repository has its own profile and helper. Git selects the helper from the
current repository, while `gh` selects the profile from the current shell.
Neither operation changes the shared host-level active account. A Codex or
other agent should source `gh git shell-init` in its shell, or evaluate
`gh git env --shell <shell>` before invoking ordinary `gh` commands.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
```

Tests use temporary repositories and isolated Git configuration. They cover
binding/unbinding, account metadata and noreply email, malformed config,
credential masking, HTTPS helper behavior, SSH alias detection, and parallel
repository isolation.
