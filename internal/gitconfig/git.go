package gitconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	IdentityKey = "github.identity"
	HostKey     = "github.host"
	IncludePath = "gh-git/git-config"
)

type CommandError struct {
	Args     []string
	ExitCode int
	err      error
}

func (e *CommandError) Error() string {
	if e.ExitCode >= 0 {
		return fmt.Sprintf("git command failed with exit code %d", e.ExitCode)
	}
	return "git command failed"
}

func (e *CommandError) Unwrap() error { return e.err }

type Client struct {
	Binary  string
	WorkDir string
	Env     []string
}

type Paths struct {
	Root          string
	ConfigPath    string
	ConfigDir     string
	GeneratedDir  string
	ProfileDir    string
	ProfileMarker string
	HostsFile     string
	ProfileConfig string
	GitConfigFile string
	StateFile     string
}

func New() *Client {
	workDir, _ := os.Getwd()
	return &Client{Binary: "git", WorkDir: workDir, Env: os.Environ()}
}

func (c *Client) run(ctx context.Context, root string, args []string, stdin io.Reader) ([]byte, error) {
	if c.Binary == "" {
		c.Binary = "git"
	}
	commandArgs := append([]string{"-C", root}, args...)
	command := exec.CommandContext(ctx, c.Binary, commandArgs...)
	command.Env = c.Env
	command.Stdin = stdin
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		exitCode := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
		return stdout.Bytes(), &CommandError{Args: commandArgs, ExitCode: exitCode, err: err}
	}
	return stdout.Bytes(), nil
}

func (c *Client) runFrom(ctx context.Context, dir string, args []string) ([]byte, error) {
	if c.Binary == "" {
		c.Binary = "git"
	}
	command := exec.CommandContext(ctx, c.Binary, args...)
	command.Dir = dir
	command.Env = c.Env
	command.Stdin = nil
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		exitCode := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
		return stdout.Bytes(), &CommandError{Args: args, ExitCode: exitCode, err: err}
	}
	return stdout.Bytes(), nil
}

func (c *Client) Root(ctx context.Context) (string, error) {
	if c.WorkDir == "" {
		var err error
		c.WorkDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
	}
	out, err := c.runFrom(ctx, c.WorkDir, []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		return "", errors.New("not inside a Git repository")
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

func (c *Client) Paths(ctx context.Context, root string) (Paths, error) {
	out, err := c.run(ctx, root, []string{"rev-parse", "--git-path", "config"}, nil)
	if err != nil {
		return Paths{}, fmt.Errorf("find Git config: %w", err)
	}
	configPath := strings.TrimSpace(string(out))
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(root, configPath)
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve Git config path: %w", err)
	}
	configDir := filepath.Dir(configPath)
	generatedDir := filepath.Join(configDir, "gh-git")
	profileDir := filepath.Join(generatedDir, "gh-config")
	return Paths{
		Root:          root,
		ConfigPath:    configPath,
		ConfigDir:     configDir,
		GeneratedDir:  generatedDir,
		ProfileDir:    profileDir,
		ProfileMarker: filepath.Join(profileDir, ".gh-git-profile"),
		HostsFile:     filepath.Join(profileDir, "hosts.yml"),
		ProfileConfig: filepath.Join(profileDir, "config.yml"),
		GitConfigFile: filepath.Join(generatedDir, "git-config"),
		StateFile:     filepath.Join(generatedDir, "state.json"),
	}, nil
}

func (c *Client) localValues(ctx context.Context, root, key string) ([]string, error) {
	out, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--get-all", key}, nil)
	if err != nil {
		var commandError *CommandError
		if errors.As(err, &commandError) && commandError.ExitCode == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("read local Git config %s: %w", key, err)
	}
	return splitLines(out), nil
}

func (c *Client) effectiveValues(ctx context.Context, root, key string) ([]string, error) {
	out, err := c.run(ctx, root, []string{"config", "--get-all", key}, nil)
	if err != nil {
		var commandError *CommandError
		if errors.As(err, &commandError) && commandError.ExitCode == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("read Git config %s: %w", key, err)
	}
	return splitLines(out), nil
}

func (c *Client) LocalValues(ctx context.Context, root, key string) ([]string, error) {
	return c.localValues(ctx, root, key)
}

func (c *Client) EffectiveValues(ctx context.Context, root, key string) ([]string, error) {
	return c.effectiveValues(ctx, root, key)
}

func (c *Client) setLocal(ctx context.Context, root, key string, values []string) error {
	if _, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--unset-all", key}, nil); err != nil {
		var commandError *CommandError
		if !errors.As(err, &commandError) || (commandError.ExitCode != 1 && commandError.ExitCode != 5) {
			return fmt.Errorf("clear local Git config %s: %w", key, err)
		}
	}
	for _, value := range values {
		if _, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--add", key, value}, nil); err != nil {
			return fmt.Errorf("write local Git config %s: %w", key, err)
		}
	}
	return nil
}

func (c *Client) ReplaceLocal(ctx context.Context, root, key string, values []string) error {
	return c.setLocal(ctx, root, key, values)
}

func (c *Client) AddLocal(ctx context.Context, root, key, value string) error {
	if _, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--add", key, value}, nil); err != nil {
		return fmt.Errorf("add local Git config %s: %w", key, err)
	}
	return nil
}

func (c *Client) UnsetMatchingLocal(ctx context.Context, root, key, pattern string) error {
	if _, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--unset-all", key, pattern}, nil); err != nil {
		var commandError *CommandError
		if errors.As(err, &commandError) && (commandError.ExitCode == 1 || commandError.ExitCode == 5) {
			return nil
		}
		return fmt.Errorf("remove local Git config %s: %w", key, err)
	}
	return nil
}

func (c *Client) Remotes(ctx context.Context, root string) ([]Remote, error) {
	out, err := c.run(ctx, root, []string{"config", "--local", "--no-includes", "--get-regexp", `^remote\..*\.url$`}, nil)
	if err != nil {
		var commandError *CommandError
		if errors.As(err, &commandError) && commandError.ExitCode == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("read Git remotes: %w", err)
	}

	remotes := make([]Remote, 0)
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		value := strings.TrimSpace(strings.TrimPrefix(line, key))
		parts := strings.Split(key, ".")
		if len(parts) != 3 || parts[0] != "remote" || parts[2] != "url" {
			continue
		}
		remote, parseErr := ParseRemote(parts[1], value)
		if parseErr != nil {
			continue
		}
		remotes = append(remotes, remote)
	}
	sort.SliceStable(remotes, func(i, j int) bool {
		if remotes[i].Name == "origin" {
			return true
		}
		if remotes[j].Name == "origin" {
			return false
		}
		return remotes[i].Name < remotes[j].Name
	})
	return remotes, nil
}

func splitLines(data []byte) []string {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
