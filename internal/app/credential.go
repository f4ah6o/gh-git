package app

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func (a *App) Credential(ctx context.Context, operation string, input io.Reader) error {
	// Git invokes store and erase after a successful operation. gh-git keeps
	// neither operation persistent because gh's account store is authoritative.
	if operation == "store" || operation == "erase" {
		return nil
	}

	root, err := a.Git.Root(ctx)
	if err != nil {
		return nil
	}
	binding, err := loadBinding(ctx, a.Git, root)
	if err != nil || binding == nil {
		return err
	}
	protocol, host, username, err := parseCredentialInput(input)
	if err != nil {
		return err
	}
	if protocol != "https" {
		return nil
	}
	if host == "" {
		host = binding.Host
	}
	if !strings.EqualFold(host, binding.Host) {
		return nil
	}
	if username != "" && !strings.EqualFold(username, binding.Account) && !strings.EqualFold(username, "x-access-token") {
		return nil
	}
	token, err := a.Auth.Token(ctx, binding.Host, binding.Account)
	if err != nil {
		return err
	}
	if strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("stored GitHub credential contains an invalid value")
	}
	_, err = fmt.Fprintf(a.Out, "protocol=https\nhost=%s\nusername=%s\npassword=%s\n\n", binding.Host, binding.Account, token)
	return err
}
