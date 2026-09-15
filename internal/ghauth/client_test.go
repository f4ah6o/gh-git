package ghauth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizedEnvRemovesCredentialAndDebugVariables(t *testing.T) {
	got := sanitizedEnv([]string{
		"PATH=/bin",
		"GH_TOKEN=secret",
		"GITHUB_TOKEN=other",
		"GH_DEBUG=api",
		"DEBUG=1",
		"TERM=xterm",
	}, "replacement")
	joined := strings.Join(got, "\n")
	for _, unwanted := range []string{"GH_TOKEN=secret", "GITHUB_TOKEN=other", "GH_DEBUG=api", "DEBUG=1"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("sanitized env contains %q: %q", unwanted, joined)
		}
	}
	if !strings.Contains(joined, "GH_TOKEN=replacement") || !strings.Contains(joined, "PATH=/bin") {
		t.Fatalf("sanitized env = %q", joined)
	}
}

func TestSanitizedEnvUsesCurrentEnvironmentWhenBaseIsNil(t *testing.T) {
	old := os.Getenv("PATH")
	t.Setenv("PATH", old)
	if len(sanitizedEnv(nil, "")) == 0 {
		t.Fatal("sanitizedEnv(nil, \"\") returned an empty environment")
	}
}

func TestBaseConfigEnvRestoresThePreHookConfig(t *testing.T) {
	got := baseConfigEnv([]string{
		"GH_CONFIG_DIR=/repo/.git/gh-git/gh-config",
		"GH_GIT_SAVED_CONFIG_DIR_SET=1",
		"GH_GIT_SAVED_CONFIG_DIR=/home/user/.config/gh",
		"PATH=/bin",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "/repo/.git/gh-git/gh-config") || !strings.Contains(joined, "GH_CONFIG_DIR=/home/user/.config/gh") {
		t.Fatalf("base config env = %q", joined)
	}

	got = baseConfigEnv([]string{"GH_CONFIG_DIR=/repo/.git/gh-git/gh-config", "GH_GIT_SAVED_CONFIG_DIR_SET=0"})
	for _, entry := range got {
		if strings.HasPrefix(entry, "GH_CONFIG_DIR=") {
			t.Fatalf("default config should be restored by unsetting GH_CONFIG_DIR: %q", got)
		}
	}
}

func TestTokenUsesThePreHookConfig(t *testing.T) {
	temporary := t.TempDir()
	capture := filepath.Join(temporary, "config-dir")
	binary := filepath.Join(temporary, "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$GH_CONFIG_DIR\" > \"$CAPTURE\"\nprintf 'token-from-keyring\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	client := &Client{
		Binary: binary,
		Env: []string{
			"GH_CONFIG_DIR=/repo/.git/gh-git/gh-config",
			"GH_GIT_SAVED_CONFIG_DIR_SET=1",
			"GH_GIT_SAVED_CONFIG_DIR=/home/user/.config/gh",
			"CAPTURE=" + capture,
		},
	}
	token, err := client.Token(context.Background(), "github.com", "github-username")
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-from-keyring" {
		t.Fatalf("token = %q", token)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "/home/user/.config/gh" {
		t.Fatalf("child GH_CONFIG_DIR = %q", data)
	}
}
