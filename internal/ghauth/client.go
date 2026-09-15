package ghauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/f4ah6o/gh-git/internal/identity"
)

type Account struct {
	Host        string `json:"host"`
	Login       string `json:"login"`
	Active      bool   `json:"active"`
	State       string `json:"state"`
	TokenSource string `json:"tokenSource"`
	GitProtocol string `json:"gitProtocol"`
}

type Client struct {
	Binary string
	Env    []string
}

func New() *Client {
	return &Client{Binary: "gh", Env: os.Environ()}
}

func (c *Client) command(ctx context.Context, args []string, token string) ([]byte, error) {
	if c.Binary == "" {
		c.Binary = "gh"
	}
	command := exec.CommandContext(ctx, c.Binary, args...)
	command.Env = sanitizedEnv(c.Env, token)
	command.Stderr = io.Discard
	var stdout bytes.Buffer
	command.Stdout = &stdout
	if err := command.Run(); err != nil {
		return nil, errors.New("GitHub CLI command failed")
	}
	return stdout.Bytes(), nil
}

// Token resolves a specific account through gh's secure account storage. It
// never changes the host's active account and never includes the token in an
// error string.
func (c *Client) Token(ctx context.Context, host, account string) (string, error) {
	// A bound shell temporarily points GH_CONFIG_DIR at the tokenless
	// repository profile. Resolve the requested account from the user's
	// original gh config so Git helpers and gh-git commands keep working while
	// the shell hook is active.
	tokenClient := *c
	tokenClient.Env = baseConfigEnv(c.Env)
	out, err := tokenClient.command(ctx, []string{"auth", "token", "--hostname", host, "--user", account}, "")
	if err != nil {
		return "", fmt.Errorf("no stored GitHub credential for account %q on %q", account, host)
	}
	token := strings.TrimSpace(string(out))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", fmt.Errorf("no stored GitHub credential for account %q on %q", account, host)
	}
	return token, nil
}

func (c *Client) Profile(ctx context.Context, host, account string) (identity.Profile, error) {
	token, err := c.Token(ctx, host, account)
	if err != nil {
		return identity.Profile{}, err
	}
	out, err := c.command(ctx, []string{"api", "--hostname", host, "user"}, token)
	if err != nil {
		return identity.Profile{}, fmt.Errorf("read GitHub profile for account %q on %q: %w", account, host, err)
	}
	var profile identity.Profile
	if err := json.Unmarshal(out, &profile); err != nil {
		return identity.Profile{}, fmt.Errorf("decode GitHub profile: %w", err)
	}
	return profile, nil
}

func (c *Client) Accounts(ctx context.Context, host string) ([]Account, error) {
	// The shell hook intentionally narrows GH_CONFIG_DIR inside a bound repo.
	// Account discovery should still show the user's complete account set, so
	// use the saved pre-hook config location for this read-only command.
	accountsClient := *c
	accountsClient.Env = baseConfigEnv(c.Env)
	out, err := accountsClient.command(ctx, []string{"auth", "status", "--hostname", host, "--json", "hosts"}, "")
	if err != nil {
		return nil, fmt.Errorf("read GitHub accounts for %q: %w", host, err)
	}
	var response struct {
		Hosts map[string][]Account `json:"hosts"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, fmt.Errorf("decode GitHub account status: %w", err)
	}
	accounts := response.Hosts[host]
	for i := range accounts {
		if accounts[i].Host == "" {
			accounts[i].Host = host
		}
	}
	return accounts, nil
}

func sanitizedEnv(base []string, token string) []string {
	if base == nil {
		base = os.Environ()
	}
	result := make([]string, 0, len(base)+1)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_DEBUG", "DEBUG":
			continue
		}
		result = append(result, entry)
	}
	if token != "" {
		// The token is passed only through the child process environment. It is
		// never an argument, persisted, or included in diagnostics.
		result = append(result, "GH_TOKEN="+token)
	}
	return result
}

func baseConfigEnv(base []string) []string {
	if base == nil {
		base = os.Environ()
	}
	savedState := ""
	savedConfig := ""
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "GH_GIT_SAVED_CONFIG_DIR_SET":
			savedState = value
		case "GH_GIT_SAVED_CONFIG_DIR":
			savedConfig = value
		}
	}
	if savedState == "" {
		return base
	}
	result := make([]string, 0, len(base)+1)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && (key == "GH_CONFIG_DIR" || key == "GH_GIT_SAVED_CONFIG_DIR_SET" || key == "GH_GIT_SAVED_CONFIG_DIR") {
			continue
		}
		result = append(result, entry)
	}
	if savedState == "1" {
		result = append(result, "GH_CONFIG_DIR="+savedConfig)
	}
	return result
}
