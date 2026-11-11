package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/f4ah6o/gh-git/internal/ghauth"
	"github.com/f4ah6o/gh-git/internal/gitconfig"
	"github.com/f4ah6o/gh-git/internal/identity"
)

const (
	cloneCredentialHelper = "!gh git credential --clone"
	cloneHostEnv          = "GH_GIT_CLONE_HOST"
	cloneAccountEnv       = "GH_GIT_CLONE_ACCOUNT"
	cloneUsage            = "usage: gh git clone <owner/repo> [<directory>] [--account <login>] [--hostname <host>]"
)

var errCloneHelp = errors.New("clone help requested")

type cloneOptions struct {
	owner       string
	repo        string
	host        string
	account     string
	destination string
}

// CloneBindError reports that the clone succeeded but the repository could not
// be bound. The clone is intentionally kept so the user can finish binding.
type CloneBindError struct {
	Destination string
	Account     string
	Err         error
}

func (e *CloneBindError) Error() string {
	return fmt.Sprintf("binding failed after clone; repository kept at %s", e.Destination)
}

func (e *CloneBindError) Unwrap() error { return e.Err }

func (a *App) runCloneCommand(ctx context.Context, args []string, input io.Reader) error {
	if len(args) == 0 {
		return errors.New(cloneUsage)
	}
	options, err := parseCloneArgs(args)
	if errors.Is(err, errCloneHelp) {
		return a.printCloneHelp()
	}
	if err != nil {
		return err
	}
	return a.Clone(ctx, options, input)
}

func (a *App) printCloneHelp() error {
	_, err := io.WriteString(a.Out, `gh git clone — clone a GitHub repository and bind it to an account

Usage:
  gh git clone <owner/repo> [<directory>] [--account <login>] [--hostname <host>]
  gh git -- clone <git arguments...>

Clones https://<host>/<owner>/<repo>.git with the selected account's stored
credential, then runs the normal bind operation against the new repository.
The current working directory is not changed.

Account selection:
  --account <login>  Use this authenticated account.
  Otherwise, <owner> is used when it is an authenticated, usable account.
  Otherwise, the only usable account is used.
  If several accounts are usable, the command fails and asks for --account.
  Organizations and unauthenticated owners follow the same fallback.

The destination defaults to <repo> in the current directory. An existing
destination is never overwritten. If cloning succeeds but binding fails, the
clone is kept and a recovery command is printed.

Use `+"`gh git -- clone ...`"+` for arbitrary Git URLs or native Git behavior.
`)
	return err
}

func parseCloneArgs(args []string) (cloneOptions, error) {
	options := cloneOptions{host: defaultGitHost}
	positional := make([]string, 0, 2)
	flagsDone := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if flagsDone {
			positional = append(positional, arg)
			continue
		}
		switch {
		case arg == "--":
			flagsDone = true
		case arg == "--help" || arg == "-h":
			return cloneOptions{}, errCloneHelp
		case arg == "--account":
			if i+1 >= len(args) {
				return cloneOptions{}, errors.New("--account requires a value")
			}
			i++
			options.account = args[i]
		case strings.HasPrefix(arg, "--account="):
			options.account = strings.TrimPrefix(arg, "--account=")
			if options.account == "" {
				return cloneOptions{}, errors.New("--account requires a value")
			}
		case arg == "--hostname" || arg == "--host":
			if i+1 >= len(args) {
				return cloneOptions{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			options.host = args[i]
		case strings.HasPrefix(arg, "--hostname="):
			options.host = strings.TrimPrefix(arg, "--hostname=")
			if options.host == "" {
				return cloneOptions{}, errors.New("--hostname requires a value")
			}
		case strings.HasPrefix(arg, "--host="):
			options.host = strings.TrimPrefix(arg, "--host=")
			if options.host == "" {
				return cloneOptions{}, errors.New("--host requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return cloneOptions{}, fmt.Errorf("unknown clone flag %q; use `gh git -- clone ...` for native Git flags", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) == 0 || len(positional) > 2 {
		return cloneOptions{}, errors.New(cloneUsage)
	}
	owner, repo, err := parseRepositorySpec(positional[0])
	if err != nil {
		return cloneOptions{}, err
	}
	options.owner = owner
	options.repo = repo
	if len(positional) == 2 {
		options.destination = positional[1]
	}
	return options, nil
}

func parseRepositorySpec(spec string) (string, string, error) {
	parts := strings.Split(spec, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected <owner>/<repo>, got %q; use `gh git -- clone <url>` for arbitrary Git URLs", spec)
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if err := identity.ValidateLogin(owner); err != nil {
		return "", "", err
	}
	if err := identity.ValidateRepositoryName(repo); err != nil {
		return "", "", err
	}
	return owner, repo, nil
}

// Clone clones owner/repo with the selected account's credential and then
// binds the new checkout. It never changes the process working directory.
func (a *App) Clone(ctx context.Context, options cloneOptions, input io.Reader) error {
	host := strings.ToLower(strings.TrimSpace(options.host))
	if err := identity.ValidateHost(host); err != nil {
		return err
	}
	workDir, err := a.cloneWorkDir()
	if err != nil {
		return err
	}
	destination := cloneDestination(workDir, options.destination, options.repo)
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination %s already exists; choose another directory", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination %s: %w", destination, err)
	}
	account, err := a.resolveCloneAccount(ctx, host, options.owner, options.account)
	if err != nil {
		return err
	}

	url := cloneURL(host, options.owner, options.repo)
	if _, err := fmt.Fprintf(a.Out, "Cloning %s/%s into %s as %s...\n", options.owner, options.repo, destination, account); err != nil {
		return err
	}
	if err := a.Git.Run(ctx, workDir, cloneGitArgs(url, destination), cloneGitEnv(a.Git.Env, host, account), input, a.Out, a.Err); err != nil {
		var commandError *gitconfig.CommandError
		if errors.As(err, &commandError) && commandError.ExitCode > 0 {
			return &GitExitError{Status: commandError.ExitCode}
		}
		return fmt.Errorf("run git clone: %w", err)
	}
	if _, err := fmt.Fprintf(a.Out, "Cloned %s/%s into %s.\n", options.owner, options.repo, destination); err != nil {
		return err
	}
	if err := a.BindAt(ctx, destination, account, host); err != nil {
		a.reportCloneBindFailure(options, host, destination, account, err)
		return &CloneBindError{Destination: destination, Account: account, Err: err}
	}
	return nil
}

func (a *App) cloneWorkDir() (string, error) {
	dir := ""
	if a.Git != nil {
		dir = a.Git.WorkDir
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	return absolute, nil
}

func cloneDestination(workDir, destination, repo string) string {
	if destination == "" {
		destination = repo
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(workDir, destination)
	}
	return filepath.Clean(destination)
}

func cloneURL(host, owner, repo string) string {
	return fmt.Sprintf("https://%s/%s/%s.git", host, owner, repo)
}

func cloneGitArgs(url, destination string) []string {
	return []string{
		"-c", "credential.helper=",
		"-c", "credential.helper=" + cloneCredentialHelper,
		"clone",
		"--",
		url,
		destination,
	}
}

// cloneGitEnv removes ambient credential sources so the clone can only use the
// selected account through the temporary credential helper.
func cloneGitEnv(base []string, host, account string) []string {
	if base == nil {
		base = os.Environ()
	}
	result := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case cloneHostEnv, cloneAccountEnv,
			"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN",
			"GIT_ASKPASS", "SSH_ASKPASS", "GIT_TERMINAL_PROMPT", "GIT_TRACE_REDACT":
			continue
		}
		result = append(result, entry)
	}
	result = append(result,
		cloneHostEnv+"="+host,
		cloneAccountEnv+"="+account,
		"GIT_TERMINAL_PROMPT=0",
	)
	return result
}

func (a *App) resolveCloneAccount(ctx context.Context, host, owner, requested string) (string, error) {
	if requested != "" {
		if err := identity.ValidateLogin(requested); err != nil {
			return "", err
		}
	}
	accounts, err := a.Auth.Accounts(ctx, host)
	if err != nil {
		return "", err
	}
	candidates := usableCloneAccounts(accounts)
	if requested != "" {
		login, ok := findCloneAccount(candidates, requested)
		if !ok {
			return "", fmt.Errorf("account %q is not an authenticated, usable account on %s; run `gh auth login --hostname %s` or choose another --account", requested, host, host)
		}
		if _, err := a.Auth.Token(ctx, host, login); err != nil {
			return "", fmt.Errorf("account %q has no usable credential on %s: %w", login, host, err)
		}
		return login, nil
	}

	ownerUnusable := false
	if login, ok := findCloneAccount(candidates, owner); ok {
		if _, err := a.Auth.Token(ctx, host, login); err == nil {
			return login, nil
		}
		ownerUnusable = true
		candidates = removeCloneAccount(candidates, login)
	}
	switch len(candidates) {
	case 0:
		if ownerUnusable {
			return "", fmt.Errorf("account %q is authenticated on %s but has no usable credential; run `gh auth login --hostname %s`", owner, host, host)
		}
		return "", fmt.Errorf("no authenticated GitHub account on %s; run `gh auth login --hostname %s`", host, host)
	case 1:
		login := candidates[0].Login
		if _, err := a.Auth.Token(ctx, host, login); err != nil {
			return "", fmt.Errorf("account %q has no usable credential on %s: %w", login, host, err)
		}
		return login, nil
	default:
		logins := make([]string, len(candidates))
		for i, account := range candidates {
			logins[i] = account.Login
		}
		return "", fmt.Errorf("cannot choose a binding account for %s on %s: multiple accounts are eligible (%s); specify --account <login>", owner, host, strings.Join(logins, ", "))
	}
}

// usableCloneAccounts filters gh's account list to logins that gh reports as
// authenticated. The selected account's token is verified separately before
// cloning, so this function never reads credentials for unused accounts.
func usableCloneAccounts(accounts []ghauth.Account) []ghauth.Account {
	result := make([]ghauth.Account, 0, len(accounts))
	seen := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		login := strings.TrimSpace(account.Login)
		if login == "" || identity.ValidateLogin(login) != nil {
			continue
		}
		if account.State != "" && !strings.EqualFold(account.State, "success") {
			continue
		}
		key := strings.ToLower(login)
		if seen[key] {
			continue
		}
		seen[key] = true
		account.Login = login
		result = append(result, account)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Login) < strings.ToLower(result[j].Login)
	})
	return result
}

func findCloneAccount(accounts []ghauth.Account, login string) (string, bool) {
	for _, account := range accounts {
		if strings.EqualFold(account.Login, login) {
			return account.Login, true
		}
	}
	return "", false
}

func removeCloneAccount(accounts []ghauth.Account, login string) []ghauth.Account {
	result := accounts[:0]
	for _, account := range accounts {
		if !strings.EqualFold(account.Login, login) {
			result = append(result, account)
		}
	}
	return result
}

func (a *App) reportCloneBindFailure(options cloneOptions, host, destination, account string, bindErr error) {
	_, _ = fmt.Fprintf(a.Err, "warning: cloned %s/%s into %s, but binding to %s failed: %v\n", options.owner, options.repo, destination, account, bindErr)
	_, _ = fmt.Fprintf(a.Err, "The clone was kept. Finish binding with:\n  %s\n", cloneRecoveryCommand(host, destination, account))
}

func cloneRecoveryCommand(host, destination, account string) string {
	command := fmt.Sprintf("cd %s && gh git bind %s", shellQuote(destination), shellQuote(account))
	if !strings.EqualFold(host, defaultGitHost) {
		command += " --hostname " + shellQuote(host)
	}
	return command
}
