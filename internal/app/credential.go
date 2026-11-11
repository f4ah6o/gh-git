package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/f4ah6o/gh-git/internal/identity"
)

func (a *App) Credential(ctx context.Context, mode, operation string, input io.Reader) error {
	// Git invokes store and erase after a successful operation. gh-git keeps
	// neither operation persistent because gh's account store is authoritative.
	if operation == "store" || operation == "erase" {
		return nil
	}
	switch mode {
	case "managed":
		return a.managedCredential(ctx, input)
	case "clone":
		return a.cloneCredential(ctx, input)
	default:
		return fmt.Errorf("unsupported credential mode %q", mode)
	}
}

func (a *App) managedCredential(ctx context.Context, input io.Reader) error {
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

// cloneCredential is the temporary helper used only for `gh git clone`. It
// resolves the account selected by the clone command from gh's secure store
// and never consults the host's active account.
func (a *App) cloneCredential(ctx context.Context, input io.Reader) error {
	host := strings.ToLower(strings.TrimSpace(os.Getenv(cloneHostEnv)))
	account := strings.TrimSpace(os.Getenv(cloneAccountEnv))
	if err := identity.ValidateHost(host); err != nil {
		return errors.New("clone credential helper is missing a valid host")
	}
	if err := identity.ValidateLogin(account); err != nil {
		return errors.New("clone credential helper is missing a valid account")
	}
	protocol, requestHost, username, err := parseCredentialInput(input)
	if err != nil {
		return err
	}
	if protocol != "https" {
		return nil
	}
	if requestHost == "" {
		requestHost = host
	}
	if !strings.EqualFold(requestHost, host) {
		return nil
	}
	if username != "" && !strings.EqualFold(username, account) && !strings.EqualFold(username, "x-access-token") {
		return nil
	}
	token, err := a.Auth.Token(ctx, host, account)
	if err != nil {
		return err
	}
	if strings.ContainsAny(token, "\r\n") {
		return errors.New("stored GitHub credential contains an invalid value")
	}
	_, err = fmt.Fprintf(a.Out, "protocol=https\nhost=%s\nusername=%s\npassword=%s\n\n", host, account, token)
	return err
}
