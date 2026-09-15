package sshconfig

import (
	"strings"
	"testing"
)

func TestHasHostAlias(t *testing.T) {
	config := `
Host github-github-username
  HostName github.com
  IdentityFile ~/.ssh/id_ed25519_github-username

Host *.example.com
  HostName github.com
`
	got, err := hasHostAlias(strings.NewReader(config), "github-github-username", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected exact SSH alias")
	}
	got, err = hasHostAlias(strings.NewReader(config), "github-other", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("wildcard or missing SSH alias matched")
	}
}
