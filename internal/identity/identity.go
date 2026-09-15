package identity

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

var loginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// Profile is the subset of the GitHub /user response needed to configure Git.
// Email is deliberately not required for github.com because the stable
// noreply address is derived from the numeric user ID.
type Profile struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Resolved is the non-secret identity written to repository-local Git config.
type Resolved struct {
	Account string
	Host    string
	Name    string
	Email   string
}

func ValidateLogin(login string) error {
	if !loginPattern.MatchString(login) {
		return fmt.Errorf("invalid GitHub account %q", login)
	}
	return nil
}

func ValidateHost(host string) error {
	if !hostPattern.MatchString(host) || strings.Contains(host, "..") {
		return fmt.Errorf("invalid GitHub host %q", host)
	}
	return nil
}

// Resolve checks that the token-selected profile is the account requested by
// the user and derives the author fields without ever handling a credential.
func Resolve(host, requested string, profile Profile) (Resolved, error) {
	if err := ValidateHost(host); err != nil {
		return Resolved{}, err
	}
	if err := ValidateLogin(requested); err != nil {
		return Resolved{}, err
	}
	if profile.ID <= 0 {
		return Resolved{}, fmt.Errorf("GitHub profile for %q has no valid user ID", requested)
	}
	if !loginPattern.MatchString(profile.Login) {
		return Resolved{}, fmt.Errorf("GitHub returned an invalid login for %q", requested)
	}
	if !strings.EqualFold(profile.Login, requested) {
		return Resolved{}, fmt.Errorf("token belongs to GitHub account %q, not %q", profile.Login, requested)
	}

	// Use the login as the Git author name. It is the stable, unambiguous
	// account identity; a user can still change user.name locally after bind.
	name := profile.Login

	email, err := AuthorEmail(host, profile)
	if err != nil {
		return Resolved{}, err
	}

	return Resolved{
		Account: profile.Login,
		Host:    host,
		Name:    name,
		Email:   email,
	}, nil
}

func AuthorEmail(host string, profile Profile) (string, error) {
	if strings.EqualFold(host, "github.com") {
		return fmt.Sprintf("%d+%s@users.noreply.github.com", profile.ID, profile.Login), nil
	}

	email := strings.TrimSpace(profile.Email)
	if email == "" {
		return "", fmt.Errorf("GitHub did not expose an email for account %q on %q; set user.email manually", profile.Login, host)
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", fmt.Errorf("GitHub returned an unusable email for account %q", profile.Login)
	}
	return email, nil
}
