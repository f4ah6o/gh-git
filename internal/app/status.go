package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/f4ah6o/gh-git/internal/gitconfig"
	"github.com/f4ah6o/gh-git/internal/sshconfig"
)

type statusReport struct {
	Repository      string   `json:"repository"`
	Bound           bool     `json:"bound"`
	Account         string   `json:"account,omitempty"`
	Host            string   `json:"host,omitempty"`
	AuthorName      string   `json:"authorName,omitempty"`
	AuthorEmail     string   `json:"authorEmail,omitempty"`
	GitHTTPS        string   `json:"gitHTTPS,omitempty"`
	GitSSH          string   `json:"gitSSH,omitempty"`
	GHProfile       string   `json:"ghConfigDir,omitempty"`
	GHAuthAvailable bool     `json:"ghAuthAvailable"`
	State           string   `json:"state"`
	Warnings        []string `json:"warnings,omitempty"`
	RemoteSummaries []string `json:"remotes,omitempty"`
}

func (a *App) Status(ctx context.Context, jsonOutput bool) error {
	report, err := a.buildStatus(ctx)
	if err != nil {
		return err
	}
	if jsonOutput {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintf(a.Out, "%s\n", data)
		return err
	}
	return a.printStatus(report)
}

func (a *App) buildStatus(ctx context.Context) (statusReport, error) {
	root, err := a.Git.Root(ctx)
	if err != nil {
		return statusReport{}, err
	}
	paths, err := a.Git.Paths(ctx, root)
	if err != nil {
		return statusReport{}, err
	}
	if err := checkGeneratedDirectories(paths); err != nil {
		return statusReport{}, err
	}
	remotes, err := a.Git.Remotes(ctx, root)
	if err != nil {
		return statusReport{}, err
	}
	report := statusReport{Repository: root, State: "unbound", Warnings: make([]string, 0)}
	for _, remote := range remotes {
		protocol := remote.Scheme
		if protocol == "" {
			protocol = "unknown"
		}
		report.RemoteSummaries = append(report.RemoteSummaries, fmt.Sprintf("%s=%s://%s", remote.Name, protocol, remote.Host))
	}
	binding, err := loadBinding(ctx, a.Git, root)
	if err != nil {
		return statusReport{}, err
	}
	if binding == nil {
		return report, nil
	}
	report.Bound = true
	report.Account = binding.Account
	report.Host = binding.Host
	name, _ := a.Git.EffectiveValues(ctx, root, "user.name")
	email, _ := a.Git.EffectiveValues(ctx, root, "user.email")
	if len(name) > 0 {
		report.AuthorName = name[len(name)-1]
	}
	if len(email) > 0 {
		report.AuthorEmail = email[len(email)-1]
	}

	state, stateErr := loadState(paths.StateFile)
	if stateErr != nil {
		return statusReport{}, stateErr
	}
	if state == nil {
		report.State = "manual-or-incomplete"
		report.Warnings = append(report.Warnings, "no gh-git state file; unbind will not guess which author values to remove")
	} else {
		report.State = "managed"
	}
	markerData, markerErr := os.ReadFile(paths.ProfileMarker)
	hostsData, hostsErr := os.ReadFile(paths.HostsFile)
	if markerErr != nil || !ownedProfileMarker(markerData) || hostsErr != nil || !profileTokenless(hostsData) {
		report.Warnings = append(report.Warnings, "tokenless GH_CONFIG_DIR profile is not available; direct gh commands need shell setup")
	} else {
		report.GHProfile = paths.ProfileDir
	}

	includePaths, includeErr := a.Git.LocalValues(ctx, root, "include.path")
	if includeErr != nil {
		return statusReport{}, includeErr
	}
	if !containsValue(includePaths, gitconfig.IncludePath) {
		report.Warnings = append(report.Warnings, "repository-local Git credential helper is not installed")
	}

	hasHTTPS, hasSSH := false, false
	for _, remote := range remotes {
		if !strings.EqualFold(remote.Host, binding.Host) {
			if remote.Host != "" {
				report.Warnings = append(report.Warnings, fmt.Sprintf("remote %s targets %s, not bound host %s", remote.Name, remote.Host, binding.Host))
			}
			continue
		}
		hasHTTPS = hasHTTPS || remote.HTTPS()
		hasSSH = hasSSH || remote.SSH
		if !remote.HTTPS() && !remote.SSH {
			report.Warnings = append(report.Warnings, fmt.Sprintf("remote %s uses unsupported Git protocol %q", remote.Name, remote.Scheme))
		}
	}
	if hasHTTPS {
		report.GitHTTPS = "repo-local gh git credential helper"
	}
	if hasSSH {
		alias := "github-" + binding.Account
		if ok, aliasErr := existingSSHAlias(alias, binding.Host); aliasErr == nil && ok {
			report.GitSSH = "existing SSH alias " + alias
		} else {
			report.GitSSH = "canonical SSH host; key selection is not bound"
			report.Warnings = append(report.Warnings, "SSH remote needs an existing account-specific alias such as github-"+binding.Account)
		}
	}
	if _, tokenErr := a.Auth.Token(ctx, binding.Host, binding.Account); tokenErr != nil {
		report.Warnings = append(report.Warnings, "stored credential for the bound account is unavailable")
	} else {
		report.GHAuthAvailable = true
	}
	return report, nil
}

func existingSSHAlias(alias, host string) (bool, error) {
	return sshconfig.HasHostAlias(alias, host)
}

func (a *App) printStatus(report statusReport) error {
	if !report.Bound {
		_, err := fmt.Fprintf(a.Out, "Repository: %s\nBinding: unbound\n", report.Repository)
		return err
	}
	if _, err := fmt.Fprintf(a.Out, "Repository: %s\nBinding: %s account %s (%s)\n", report.Repository, report.Host, report.Account, report.State); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.Out, "Git author: %s <%s>\n", report.AuthorName, report.AuthorEmail); err != nil {
		return err
	}
	if report.GitHTTPS != "" {
		if _, err := fmt.Fprintf(a.Out, "Git HTTPS: %s\n", report.GitHTTPS); err != nil {
			return err
		}
	}
	if report.GitSSH != "" {
		if _, err := fmt.Fprintf(a.Out, "Git SSH: %s\n", report.GitSSH); err != nil {
			return err
		}
	}
	if report.GHProfile != "" {
		if _, err := fmt.Fprintf(a.Out, "gh profile: %s (shell hook selects GH_CONFIG_DIR)\n", report.GHProfile); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(a.Out, "gh profile: unavailable"); err != nil {
		return err
	}
	if report.GHAuthAvailable {
		if _, err := fmt.Fprintln(a.Out, "gh credential: available in secure account storage"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(a.Out, "gh credential: unavailable"); err != nil {
		return err
	}
	for _, remote := range report.RemoteSummaries {
		if _, err := fmt.Fprintf(a.Out, "Remote: %s\n", remote); err != nil {
			return err
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(a.Out, "Warning: %s.\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Doctor(ctx context.Context) error {
	report, err := a.buildStatus(ctx)
	if err != nil {
		return err
	}
	if err := a.printStatus(report); err != nil {
		return err
	}
	if !report.Bound {
		return errors.New("repository is not bound; run `gh git bind <account>`")
	}
	if len(report.Warnings) > 0 {
		return errors.New("gh-git doctor found configuration issues")
	}
	return nil
}
