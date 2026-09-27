package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion = 1
	fetchRefspec  = "+refs/heads/*:refs/remotes/origin/*"
	metadataName  = "gh-git/store.json"
)

type Repository struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type StoreInfo struct {
	Layout string `json:"layout"`
	Path   string `json:"path"`
}

type RemoteInfo struct {
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type RefObservation struct {
	Commit     string `json:"commit"`
	ObservedAt string `json:"observed_at,omitempty"`
}

type ErrorInfo struct {
	Code     string `json:"code"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message"`
}

type FetchInfo struct {
	LastAttemptAt string     `json:"last_attempt_at,omitempty"`
	LastSuccessAt string     `json:"last_success_at,omitempty"`
	LastError     *ErrorInfo `json:"last_error"`
}

type Result struct {
	SchemaVersion int                       `json:"schema_version"`
	Repository    Repository                `json:"repository"`
	Store         StoreInfo                 `json:"store"`
	Remote        RemoteInfo                `json:"remote"`
	Observations  map[string]RefObservation `json:"observations"`
	Fetch         FetchInfo                 `json:"fetch"`
}

type Error struct {
	Code     string
	Category string
	Message  string
}

func (e *Error) Error() string { return e.Message }

type metadata struct {
	SchemaVersion int                       `json:"schema_version"`
	Repository    Repository                `json:"repository"`
	DefaultBranch string                    `json:"default_branch,omitempty"`
	Observations  map[string]RefObservation `json:"observations,omitempty"`
	LastAttemptAt string                    `json:"last_attempt_at,omitempty"`
	LastSuccessAt string                    `json:"last_success_at,omitempty"`
	LastError     *ErrorInfo                `json:"last_error,omitempty"`
}

type Manager struct {
	Root      string
	GitBinary string
	Env       []string
	Now       func() time.Time
	RemoteURL func(Repository) string
}

func New(root string) *Manager {
	return &Manager{Root: root, GitBinary: "git", Env: os.Environ(), Now: time.Now}
}

func (m *Manager) remoteURL(repo Repository) string {
	if m.RemoteURL != nil {
		return m.RemoteURL(repo)
	}
	return repo.URL()
}

var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ParseRepository(spec string) (Repository, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Repository{}, newError("repo_ambiguous", "", "repository must be host/owner/repo, owner/repo, or a GitHub URL")
	}
	host := "github.com"
	var owner, name string

	switch {
	case strings.Contains(spec, "://"):
		u, err := url.Parse(spec)
		if err != nil || u.Hostname() == "" {
			return Repository{}, newError("repo_ambiguous", "", "invalid repository URL")
		}
		host = strings.ToLower(u.Hostname())
		parts := splitRepositoryPath(u.Path)
		if len(parts) != 2 {
			return Repository{}, newError("owner_required", "", "repository URL must include owner and repository name")
		}
		owner, name = parts[0], parts[1]
	case strings.Contains(spec, "@") && strings.Contains(spec, ":"):
		at := strings.LastIndex(spec, "@")
		colon := strings.Index(spec[at+1:], ":")
		if colon < 0 {
			return Repository{}, newError("repo_ambiguous", "", "invalid SSH repository URL")
		}
		colon += at + 1
		host = strings.ToLower(spec[at+1 : colon])
		parts := splitRepositoryPath(spec[colon+1:])
		if len(parts) != 2 {
			return Repository{}, newError("owner_required", "", "repository URL must include owner and repository name")
		}
		owner, name = parts[0], parts[1]
	default:
		parts := splitRepositoryPath(spec)
		if len(parts) == 2 {
			owner, name = parts[0], parts[1]
		} else if len(parts) == 3 {
			host, owner, name = strings.ToLower(parts[0]), parts[1], parts[2]
		} else {
			return Repository{}, newError("owner_required", "", "repository must include owner and repository name")
		}
	}

	name = strings.TrimSuffix(name, ".git")
	repo := Repository{Host: strings.ToLower(host), Owner: owner, Name: name}
	if err := validateRepository(repo); err != nil {
		return Repository{}, err
	}
	return repo, nil
}

func splitRepositoryPath(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func validateRepository(repo Repository) error {
	for label, value := range map[string]string{"host": repo.Host, "owner": repo.Owner, "repo": repo.Name} {
		if value == "" || value == "." || value == ".." || !segmentPattern.MatchString(value) {
			return newError("path_escape", "", fmt.Sprintf("invalid repository %s %q", label, value))
		}
	}
	if strings.Contains(repo.Host, "..") || strings.Contains(repo.Owner, "..") || strings.Contains(repo.Name, "..") {
		return newError("path_escape", "", "repository identity contains an unsafe path segment")
	}
	return nil
}

func (r Repository) URL() string {
	return "https://" + r.Host + "/" + r.Owner + "/" + r.Name + ".git"
}

func DefaultRoot() (string, error) {
	if value := strings.TrimSpace(os.Getenv("GH_GIT_STORE_ROOT")); value != "" {
		return filepath.Abs(value)
	}
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return filepath.Abs(filepath.Join(value, "gh-git", "stores"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "gh-git", "stores"), nil
}

func (m *Manager) StorePath(repo Repository) (string, error) {
	if err := validateRepository(repo); err != nil {
		return "", err
	}
	root := m.Root
	var err error
	if strings.TrimSpace(root) == "" {
		root, err = DefaultRoot()
		if err != nil {
			return "", err
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve store root: %w", err)
	}
	path := filepath.Join(root, repo.Host, repo.Owner, repo.Name+".git")
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", newError("path_escape", "", "repository store path escapes the configured store root")
	}
	return path, nil
}

func resolveStorePhysicalPath(path string, depth int) (string, error) {
	if depth > 256 {
		return "", fmt.Errorf("resolve store path: too many symlink levels")
	}
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve store path: %w", err)
		}
		path = absolute
	}
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path, nil
		}
		resolvedParent, err := resolveStorePhysicalPath(parent, depth+1)
		if err != nil {
			return "", err
		}
		return filepath.Join(resolvedParent, filepath.Base(path)), nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		return resolveStorePhysicalPath(link, depth+1)
	}

	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	resolvedParent, err := resolveStorePhysicalPath(parent, depth+1)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

func storePathWithinPhysicalRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil &&
		rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}

func (m *Manager) ensurePhysicalStorePath(path string) error {
	root := m.Root
	var err error
	if strings.TrimSpace(root) == "" {
		root, err = DefaultRoot()
		if err != nil {
			return err
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve store root: %w", err)
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve store path: %w", err)
	}
	if !storePathWithinPhysicalRoot(root, target) {
		return newError("path_escape", "", "repository store path escapes the configured store root")
	}

	physicalRoot, err := resolveStorePhysicalPath(root, 0)
	if err != nil {
		return fmt.Errorf("resolve physical store root: %w", err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("resolve store path containment: %w", err)
	}

	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return fmt.Errorf("inspect store path: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		physical, resolveErr := resolveStorePhysicalPath(current, 0)
		if resolveErr != nil {
			return fmt.Errorf("resolve store path: %w", resolveErr)
		}
		if !storePathWithinPhysicalRoot(physicalRoot, physical) {
			return newError("path_escape", "", "repository store path traverses a symlink outside the physical store root")
		}
	}
	return nil
}

func (m *Manager) Ensure(ctx context.Context, repo Repository) (Result, error) {
	path, err := m.StorePath(repo)
	if err != nil {
		return Result{}, err
	}
	if err := m.ensurePhysicalStorePath(path); err != nil {
		return Result{}, err
	}
	info, statErr := os.Lstat(path)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return Result{}, fmt.Errorf("create store parent: %w", err)
		}
		if _, _, err := m.runGit(ctx, "", "init", "--bare", "--quiet", path); err != nil {
			return Result{}, newError("store_conflict", "", "failed to initialize bare repository store")
		}
		if _, _, err := m.runGit(ctx, path, "remote", "add", "origin", m.remoteURL(repo)); err != nil {
			return Result{}, newError("store_conflict", "", "failed to configure origin for repository store")
		}
		if _, _, err := m.runGit(ctx, path, "config", "--replace-all", "remote.origin.fetch", fetchRefspec); err != nil {
			return Result{}, newError("store_conflict", "", "failed to configure fetch refspec for repository store")
		}
	case statErr != nil:
		return Result{}, fmt.Errorf("inspect store path: %w", statErr)
	case !info.IsDir():
		return Result{}, newError("path_occupied", "", "repository store path is occupied by a non-directory")
	default:
		if err := m.verifyStore(ctx, path, repo); err != nil {
			return Result{}, err
		}
	}
	if err := m.ensureMetadata(path, repo); err != nil {
		return Result{}, err
	}
	return m.Inspect(ctx, repo)
}

func (m *Manager) Inspect(ctx context.Context, repo Repository) (Result, error) {
	path, err := m.StorePath(repo)
	if err != nil {
		return Result{}, err
	}
	if err := m.ensurePhysicalStorePath(path); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Result{}, newError("store_missing", "", "repository store does not exist")
	} else if err != nil {
		return Result{}, fmt.Errorf("inspect store: %w", err)
	}
	if err := m.verifyStore(ctx, path, repo); err != nil {
		return Result{}, err
	}
	meta, err := readMetadata(path)
	if err != nil {
		return Result{}, err
	}
	refs, err := m.currentRemoteRefs(ctx, path)
	if err != nil {
		return Result{}, err
	}
	observations := make(map[string]RefObservation, len(refs))
	for ref, commit := range refs {
		observed := RefObservation{Commit: commit}
		if prior, ok := meta.Observations[ref]; ok && prior.Commit == commit {
			observed.ObservedAt = prior.ObservedAt
		}
		observations[ref] = observed
	}
	return Result{
		SchemaVersion: SchemaVersion,
		Repository:    repo,
		Store:         StoreInfo{Layout: "bare", Path: path},
		Remote:        RemoteInfo{URL: m.remoteURL(repo), DefaultBranch: meta.DefaultBranch},
		Observations:  observations,
		Fetch: FetchInfo{
			LastAttemptAt: meta.LastAttemptAt,
			LastSuccessAt: meta.LastSuccessAt,
			LastError:     meta.LastError,
		},
	}, nil
}

func (m *Manager) Fetch(ctx context.Context, repo Repository) (Result, error) {
	path, err := m.StorePath(repo)
	if err != nil {
		return Result{}, err
	}
	if err := m.ensurePhysicalStorePath(path); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Result{}, newError("store_missing", "", "repository store does not exist")
	} else if err != nil {
		return Result{}, err
	}
	if err := m.verifyStore(ctx, path, repo); err != nil {
		return Result{}, err
	}
	meta, err := readMetadata(path)
	if err != nil {
		return Result{}, err
	}
	now := m.now().UTC().Format(time.RFC3339Nano)
	meta.LastAttemptAt = now

	_, stderr, fetchErr := m.runGit(ctx, path, "fetch", "--atomic", "--prune", "origin")
	if fetchErr != nil {
		info := classifyFetchError(stderr)
		meta.LastError = &info
		if writeErr := writeMetadata(path, meta); writeErr != nil {
			return Result{}, writeErr
		}
		result, inspectErr := m.Inspect(ctx, repo)
		if inspectErr != nil {
			return Result{}, inspectErr
		}
		return result, &Error{Code: info.Code, Category: info.Category, Message: info.Message}
	}

	defaultBranch := m.observeDefaultBranch(ctx, path)
	if defaultBranch != "" {
		meta.DefaultBranch = defaultBranch
		_, _, _ = m.runGit(ctx, path, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+defaultBranch)
	}
	refs, err := m.currentRemoteRefs(ctx, path)
	if err != nil {
		return Result{}, err
	}
	meta.Observations = make(map[string]RefObservation, len(refs))
	for ref, commit := range refs {
		meta.Observations[ref] = RefObservation{Commit: commit, ObservedAt: now}
	}
	meta.LastSuccessAt = now
	meta.LastError = nil
	if err := writeMetadata(path, meta); err != nil {
		return Result{}, err
	}
	return m.Inspect(ctx, repo)
}

func (m *Manager) verifyStore(ctx context.Context, path string, repo Repository) error {
	out, _, err := m.runGit(ctx, path, "rev-parse", "--is-bare-repository")
	if err != nil || strings.TrimSpace(out) != "true" {
		return newError("legacy_layout", "", "existing repository store path is not a bare Git repository")
	}
	origin, _, err := m.runGit(ctx, path, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(origin) != m.remoteURL(repo) {
		return newError("store_conflict", "", "existing repository store has an incompatible origin URL")
	}
	refspecs, _, err := m.runGit(ctx, path, "config", "--get-all", "remote.origin.fetch")
	if err != nil {
		return newError("store_conflict", "", "existing repository store has no compatible fetch refspec")
	}
	compatible := false
	for _, value := range strings.Split(strings.TrimSpace(refspecs), "\n") {
		if strings.TrimSpace(value) == fetchRefspec {
			compatible = true
			break
		}
	}
	if !compatible {
		return newError("store_conflict", "", "existing repository store has an incompatible fetch refspec")
	}
	return nil
}

func (m *Manager) currentRemoteRefs(ctx context.Context, path string) (map[string]string, error) {
	out, _, err := m.runGit(ctx, path, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes/origin")
	if err != nil {
		return nil, newError("base_unresolved", "", "failed to inspect remote-tracking refs")
	}
	result := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ref, commit, ok := strings.Cut(line, " ")
		if !ok || ref == "refs/remotes/origin/HEAD" {
			continue
		}
		result[ref] = strings.TrimSpace(commit)
	}
	return result, nil
}

func (m *Manager) observeDefaultBranch(ctx context.Context, path string) string {
	out, _, err := m.runGit(ctx, path, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" && strings.HasPrefix(fields[1], "refs/heads/") {
			return strings.TrimPrefix(fields[1], "refs/heads/")
		}
	}
	return ""
}

func (m *Manager) runGit(ctx context.Context, dir string, args ...string) (string, string, error) {
	binary := m.GitBinary
	if binary == "" {
		binary = "git"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if m.Env != nil {
		cmd.Env = m.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), redact(stderr.String()), err
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) ensureMetadata(path string, repo Repository) error {
	meta, err := readMetadata(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		meta = metadata{}
	}
	if meta.SchemaVersion != 0 && meta.SchemaVersion != SchemaVersion {
		return newError("schema_version_unsupported", "", fmt.Sprintf("unsupported store metadata schema version %d", meta.SchemaVersion))
	}
	if meta.Repository.Host != "" && meta.Repository != repo {
		return newError("store_conflict", "", "repository store metadata belongs to a different repository")
	}
	meta.SchemaVersion = SchemaVersion
	meta.Repository = repo
	if meta.Observations == nil {
		meta.Observations = map[string]RefObservation{}
	}
	return writeMetadata(path, meta)
}

func readMetadata(path string) (metadata, error) {
	data, err := os.ReadFile(filepath.Join(path, metadataName))
	if errors.Is(err, os.ErrNotExist) {
		return metadata{SchemaVersion: SchemaVersion, Observations: map[string]RefObservation{}}, nil
	}
	if err != nil {
		return metadata{}, err
	}
	var meta metadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&meta); err != nil {
		return metadata{}, newError("schema_version_unsupported", "", "repository store metadata is malformed or unsupported")
	}
	if meta.SchemaVersion != SchemaVersion {
		return metadata{}, newError("schema_version_unsupported", "", fmt.Sprintf("unsupported store metadata schema version %d", meta.SchemaVersion))
	}
	if meta.Observations == nil {
		meta.Observations = map[string]RefObservation{}
	}
	return meta, nil
}

func writeMetadata(path string, meta metadata) error {
	meta.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	target := filepath.Join(path, metadataName)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".store-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target)
}

func classifyFetchError(stderr string) ErrorInfo {
	message := strings.TrimSpace(redact(stderr))
	category := "remote"
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "could not read username"),
		strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "repository not found"):
		category = "authentication"
	case strings.Contains(lower, "could not resolve host"),
		strings.Contains(lower, "failed to connect"),
		strings.Contains(lower, "connection timed out"),
		strings.Contains(lower, "network is unreachable"):
		category = "network"
	}
	if message == "" {
		message = "git fetch failed"
	}
	return ErrorInfo{Code: "fetch_failed", Category: category, Message: message}
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:[[:space:]]*(?:bearer|basic)[[:space:]]+)[^[:space:]]+`),
	regexp.MustCompile(`(?i)(https?://)[^/@[:space:]]+:[^/@[:space:]]+@`),
	regexp.MustCompile(`(?i)((?:token|password|oauth_token)[=:][[:space:]]*)[^[:space:]]+`),
}

func redact(value string) string {
	result := value
	for _, pattern := range secretPatterns {
		result = pattern.ReplaceAllString(result, "$1<redacted>")
	}
	return result
}

func newError(code, category, message string) *Error {
	return &Error{Code: code, Category: category, Message: message}
}
