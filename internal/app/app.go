package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/f4ah6o/gh-git/internal/ghauth"
	"github.com/f4ah6o/gh-git/internal/gitconfig"
	"github.com/f4ah6o/gh-git/internal/identity"
	"github.com/f4ah6o/gh-git/internal/shell"
	"github.com/f4ah6o/gh-git/internal/sshconfig"
)

const (
	stateVersion   = 1
	gitFileMarker  = "# gh-git managed file; contains no credentials.\n"
	profileMarker  = "gh-git profile v1; contains no credentials.\n"
	profileConfig  = "version: \"1\"\n"
	defaultGitHost = "github.com"
)

type Auth interface {
	Token(context.Context, string, string) (string, error)
	Profile(context.Context, string, string) (identity.Profile, error)
	Accounts(context.Context, string) ([]ghauth.Account, error)
}

type App struct {
	Git  *gitconfig.Client
	Auth Auth
	Out  io.Writer
	Err  io.Writer
}

func New(out, errOut io.Writer) *App {
	return &App{
		Git:  gitconfig.New(),
		Auth: ghauth.New(),
		Out:  out,
		Err:  errOut,
	}
}

func (a *App) Run(ctx context.Context, args []string, input io.Reader) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		return a.printMainHelp()
	}

	switch args[0] {
	case "bind":
		account, host, err := parseBindArgs(args[1:])
		if err != nil {
			return err
		}
		return a.Bind(ctx, account, host)
	case "unbind":
		if len(args) != 1 {
			return errors.New("usage: gh git unbind")
		}
		return a.Unbind(ctx)
	case "status":
		jsonOutput, err := parseJSONFlag(args[1:])
		if err != nil {
			return err
		}
		return a.Status(ctx, jsonOutput)
	case "accounts":
		host, err := parseHostFlag(args[1:])
		if err != nil {
			return err
		}
		return a.Accounts(ctx, host)
	case "doctor":
		if len(args) != 1 {
			return errors.New("usage: gh git doctor")
		}
		return a.Doctor(ctx)
	case "env":
		shellName, err := parseShellFlag(args[1:])
		if err != nil {
			return err
		}
		return a.Env(ctx, shellName)
	case "shell-init":
		if len(args) != 2 {
			return errors.New("usage: gh git shell-init <bash|zsh|fish>")
		}
		value, err := shell.Init(args[1])
		if err != nil {
			return err
		}
		_, err = io.WriteString(a.Out, value)
		return err
	case "credential":
		operation, err := parseCredentialArgs(args[1:])
		if err != nil {
			return err
		}
		return a.Credential(ctx, operation, input)
	case "init":
		return a.InitHelp()
	default:
		return fmt.Errorf("unknown command %q; run `gh git --help`", args[0])
	}
}

func (a *App) printMainHelp() error {
	_, err := io.WriteString(a.Out, `gh git — repository-scoped GitHub identity

Bind a repository to one GitHub account without changing gh's global active account.

Usage:
  gh git bind <github-username> [--hostname <host>]
  gh git unbind
  gh git status [--json]
  gh git accounts [--hostname <host>]
  gh git doctor
  gh git env [--shell <bash|zsh|fish>]
  gh git shell-init <bash|zsh|fish>

Commands:
  bind         Bind this repository to one GitHub account.
  unbind       Remove gh-git's binding and restore prior local author values.
  status       Show binding and authentication wiring without secrets.
  accounts     List stored GitHub accounts without changing the active account.
  doctor       Explain missing or unsafe pieces of a binding.
  env          Print the tokenless GH_CONFIG_DIR profile for this repository.
  shell-init   Print an optional cd/prompt hook for direct gh invocations.

Quick start:
  eval "$(gh git shell-init bash)"  # run once per bash shell
  cd /path/to/repository
  gh git bind <github-username>
  gh git status

Use zsh or fish instead of bash for those shells. The shell hook is
needed for direct commands such as gh pr create and gh api user.
Git HTTPS commands use the repository-local credential helper automatically.

Installation:
  - Remote gh extension install requires a published CalVer Release with platform binaries.
  - For local development, build ./gh-git or install from the local repository after building it.

Safety:
  - gh-git never calls gh auth switch.
  - Tokens stay in gh's secure credential store and are never written to the repository.
  - Run gh git doctor when status reports a missing or unsafe setup.

The hidden credential command is called by Git's repository-local helper.
`)
	return err
}

func (a *App) InitHelp() error {
	_, err := io.WriteString(a.Out, "gh git init is intentionally not automatic; source `eval \"$(gh git shell-init bash)\"` once, then run `gh git bind <github-username>` in a repository.\n")
	return err
}

func parseBindArgs(args []string) (string, string, error) {
	var account string
	host := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			return "", "", errors.New("usage: gh git bind <github-username> [--hostname <host>]")
		case "--hostname", "--host":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", args[i])
			}
			i++
			host = args[i]
		case "--hostname=", "--host=":
			return "", "", fmt.Errorf("%s requires a value", args[i])
		default:
			if strings.HasPrefix(args[i], "--hostname=") {
				host = strings.TrimPrefix(args[i], "--hostname=")
			} else if strings.HasPrefix(args[i], "--host=") {
				host = strings.TrimPrefix(args[i], "--host=")
			} else if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown bind flag %q", args[i])
			} else if account == "" {
				account = args[i]
			} else {
				return "", "", errors.New("usage: gh git bind <github-username> [--hostname <host>]")
			}
		}
	}
	if account == "" {
		return "", "", errors.New("usage: gh git bind <github-username> [--hostname <host>]")
	}
	return account, host, nil
}

func parseHostFlag(args []string) (string, error) {
	host := defaultGitHost
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--hostname" || args[i] == "--host":
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i])
			}
			i++
			host = args[i]
		case strings.HasPrefix(args[i], "--hostname="):
			host = strings.TrimPrefix(args[i], "--hostname=")
		case strings.HasPrefix(args[i], "--host="):
			host = strings.TrimPrefix(args[i], "--host=")
		default:
			return "", fmt.Errorf("unknown accounts flag %q", args[i])
		}
	}
	return host, nil
}

func parseJSONFlag(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, nil
	}
	return false, fmt.Errorf("usage: gh git status [--json]")
}

func parseShellFlag(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 2 && args[0] == "--shell" {
		return args[1], nil
	}
	if len(args) == 1 && strings.HasPrefix(args[0], "--shell=") {
		return strings.TrimPrefix(args[0], "--shell="), nil
	}
	return "", errors.New("usage: gh git env [--shell <bash|zsh|fish>]")
}

func parseCredentialArgs(args []string) (string, error) {
	if len(args) == 1 && (args[0] == "get" || args[0] == "store" || args[0] == "erase") {
		return args[0], nil
	}
	if len(args) == 2 && args[0] == "--managed" && (args[1] == "get" || args[1] == "store" || args[1] == "erase") {
		return args[1], nil
	}
	return "", errors.New("unsupported Git credential operation")
}

type Binding struct {
	Account string
	Host    string
}

type configSnapshot struct {
	UserName     []string `json:"userName,omitempty"`
	UserEmail    []string `json:"userEmail,omitempty"`
	Identity     []string `json:"identity,omitempty"`
	Host         []string `json:"host,omitempty"`
	IncludePaths []string `json:"includePaths,omitempty"`
}

type bindingState struct {
	Version     int            `json:"version"`
	Account     string         `json:"account"`
	Host        string         `json:"host"`
	AuthorName  string         `json:"authorName"`
	AuthorEmail string         `json:"authorEmail"`
	Snapshot    configSnapshot `json:"snapshot"`
}

func loadBinding(ctx context.Context, git *gitconfig.Client, root string) (*Binding, error) {
	identities, err := git.LocalValues(ctx, root, gitconfig.IdentityKey)
	if err != nil {
		return nil, err
	}
	hosts, err := git.LocalValues(ctx, root, gitconfig.HostKey)
	if err != nil {
		return nil, err
	}
	if len(identities) == 0 && len(hosts) == 0 {
		return nil, nil
	}
	if len(identities) != 1 {
		return nil, errors.New("malformed repository binding: github.identity must have exactly one value")
	}
	if err := identity.ValidateLogin(identities[0]); err != nil {
		return nil, fmt.Errorf("malformed repository binding: %w", err)
	}
	host := defaultGitHost
	if len(hosts) > 1 {
		return nil, errors.New("malformed repository binding: github.host must have at most one value")
	}
	if len(hosts) == 1 {
		host = strings.ToLower(hosts[0])
		if err := identity.ValidateHost(host); err != nil {
			return nil, fmt.Errorf("malformed repository binding: %w", err)
		}
	}
	return &Binding{Account: identities[0], Host: host}, nil
}

func loadState(path string) (*bindingState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read gh-git state: %w", err)
	}
	var state bindingState
	if err := decodeState(data, &state); err != nil {
		return nil, fmt.Errorf("malformed gh-git state: %w", err)
	}
	if state.Version != stateVersion {
		return nil, fmt.Errorf("unsupported gh-git state version %d", state.Version)
	}
	if err := identity.ValidateLogin(state.Account); err != nil {
		return nil, fmt.Errorf("malformed gh-git state: %w", err)
	}
	if err := identity.ValidateHost(state.Host); err != nil {
		return nil, fmt.Errorf("malformed gh-git state: %w", err)
	}
	return &state, nil
}

func decodeState(data []byte, state *bindingState) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(state); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func captureSnapshot(ctx context.Context, git *gitconfig.Client, root string) (configSnapshot, error) {
	values := func(key string) ([]string, error) { return git.LocalValues(ctx, root, key) }
	userName, err := values("user.name")
	if err != nil {
		return configSnapshot{}, err
	}
	userEmail, err := values("user.email")
	if err != nil {
		return configSnapshot{}, err
	}
	identityValues, err := values(gitconfig.IdentityKey)
	if err != nil {
		return configSnapshot{}, err
	}
	hostValues, err := values(gitconfig.HostKey)
	if err != nil {
		return configSnapshot{}, err
	}
	includePaths, err := values("include.path")
	if err != nil {
		return configSnapshot{}, err
	}
	return configSnapshot{
		UserName: userName, UserEmail: userEmail, Identity: identityValues,
		Host: hostValues, IncludePaths: includePaths,
	}, nil
}

func restoreSnapshot(ctx context.Context, git *gitconfig.Client, root string, snapshot configSnapshot) error {
	for key, values := range map[string][]string{
		"user.name": snapshot.UserName, "user.email": snapshot.UserEmail,
		gitconfig.IdentityKey: snapshot.Identity, gitconfig.HostKey: snapshot.Host,
		"include.path": snapshot.IncludePaths,
	} {
		if err := git.ReplaceLocal(ctx, root, key, values); err != nil {
			return err
		}
	}
	return nil
}

func valuesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type fileBackup struct {
	path   string
	exists bool
	data   []byte
	mode   os.FileMode
}

func backupFile(path string) (fileBackup, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileBackup{path: path}, nil
	}
	if err != nil {
		return fileBackup{}, err
	}
	if !info.Mode().IsRegular() {
		return fileBackup{}, fmt.Errorf("generated path %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileBackup{}, err
	}
	return fileBackup{path: path, exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func restoreFile(backup fileBackup) error {
	if !backup.exists {
		if err := os.Remove(backup.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeAtomic(backup.path, backup.data, backup.mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gh-git-write-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func writeOwnedFile(path string, data []byte, owner func([]byte) bool) error {
	if existing, err := os.ReadFile(path); err == nil {
		if !owner(existing) {
			return fmt.Errorf("refusing to overwrite non-gh-git file %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

func removeOwnedFile(path string, owner func([]byte) bool) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !owner(data) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func profileYAML(host, account string) []byte {
	return []byte(fmt.Sprintf("# Generated by gh-git; contains no credentials.\n%s:\n    git_protocol: https\n    users:\n        %s: {}\n    user: %s\n", host, account, account))
}

func gitConfigText(host, account, sshAlias string) []byte {
	var builder strings.Builder
	builder.WriteString(gitFileMarker)
	builder.WriteString("[credential]\n    helper =\n    helper = !gh git credential --managed\n    useHttpPath = true\n")
	if sshAlias != "" {
		fmt.Fprintf(&builder, "[url \"git@%s:\"]\n    insteadOf = git@%s:\n", sshAlias, host)
		fmt.Fprintf(&builder, "[url \"ssh://git@%s/\"]\n    insteadOf = ssh://git@%s/\n", sshAlias, host)
	}
	return []byte(builder.String())
}

func ownedGitFile(data []byte) bool { return strings.HasPrefix(string(data), gitFileMarker) }

func ownedProfileMarker(data []byte) bool { return string(data) == profileMarker }

func ownedProfileConfig(data []byte) bool { return string(data) == profileConfig }

func ownedHosts(data []byte) bool {
	return strings.HasPrefix(string(data), "# Generated by gh-git; contains no credentials.\n")
}

func profileTokenless(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		field := strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		switch strings.ToLower(field) {
		case "oauth_token", "token", "password":
			return false
		}
	}
	return true
}

func ownedState(data []byte) bool {
	var state bindingState
	return decodeState(data, &state) == nil && state.Version == stateVersion
}

func checkGeneratedDirectories(paths gitconfig.Paths) error {
	for _, path := range []string{paths.GeneratedDir, paths.ProfileDir} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to use symlinked gh-git directory %s", path)
		}
		if !info.IsDir() {
			return fmt.Errorf("gh-git path %s is not a directory", path)
		}
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parseCredentialInput(input io.Reader) (protocol, host, username string, err error) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "protocol":
			protocol = value
		case "host":
			host = value
		case "username":
			username = value
		case "url":
			parsed, parseErr := url.Parse(value)
			if parseErr != nil {
				return "", "", "", errors.New("malformed Git credential URL")
			}
			protocol = parsed.Scheme
			host = parsed.Hostname()
			if parsed.User != nil {
				username = parsed.User.Username()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", err
	}
	return protocol, host, username, nil
}

func sshAliasFor(remotes []gitconfig.Remote, host, account string) string {
	for _, remote := range remotes {
		if remote.Host == host && remote.SSH {
			alias := "github-" + account
			if ok, err := sshconfig.HasHostAlias(alias, host); err == nil && ok {
				return alias
			}
			return ""
		}
	}
	return ""
}
