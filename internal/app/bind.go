package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/f4ah6o/gh-git/internal/gitconfig"
	"github.com/f4ah6o/gh-git/internal/identity"
)

func (a *App) Bind(ctx context.Context, account, hostOverride string) error {
	if err := identity.ValidateLogin(account); err != nil {
		return err
	}
	root, err := a.Git.Root(ctx)
	if err != nil {
		return err
	}
	paths, err := a.Git.Paths(ctx, root)
	if err != nil {
		return err
	}
	if err := checkGeneratedDirectories(paths); err != nil {
		return err
	}
	remotes, err := a.Git.Remotes(ctx, root)
	if err != nil {
		return err
	}
	host, err := chooseHost(remotes, hostOverride)
	if err != nil {
		return err
	}
	for _, remote := range remotes {
		if strings.EqualFold(remote.Host, host) && !remote.HTTPS() && !remote.SSH {
			return fmt.Errorf("remote %s uses unsupported Git protocol %q; use HTTPS or SSH", remote.Name, remote.Scheme)
		}
	}

	profile, err := a.Auth.Profile(ctx, host, account)
	if err != nil {
		return err
	}
	resolved, err := identity.Resolve(host, account, profile)
	if err != nil {
		return err
	}

	oldState, err := loadState(paths.StateFile)
	if err != nil {
		return err
	}
	current, err := captureSnapshot(ctx, a.Git, root)
	if err != nil {
		return err
	}
	if oldState == nil && containsValue(current.IncludePaths, gitconfig.IncludePath) {
		return errors.New("repository already contains gh-git/git-config without binding state; refusing to take ownership")
	}
	baseline := current
	if oldState != nil {
		if !managedSnapshotMatches(current, *oldState) {
			return errors.New("repository config changed after the previous bind; run `gh git unbind` or restore the managed values first")
		}
		baseline = oldState.Snapshot
	}

	sshAlias := sshAliasFor(remotes, host, resolved.Account)
	profileData := profileYAML(host, resolved.Account)
	gitData := gitConfigText(host, resolved.Account, sshAlias)
	if err := validateGeneratedFiles(paths, oldState, profileData); err != nil {
		return err
	}

	backups := make([]fileBackup, 0, 4)
	for _, path := range []string{paths.ProfileMarker, paths.HostsFile, paths.ProfileConfig, paths.GitConfigFile, paths.StateFile} {
		backup, backupErr := backupFile(path)
		if backupErr != nil {
			return backupErr
		}
		backups = append(backups, backup)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = restoreSnapshot(ctx, a.Git, root, current)
		for _, backup := range backups {
			_ = restoreFile(backup)
		}
	}()

	if err := writeOwnedFile(paths.ProfileMarker, []byte(profileMarker), ownedProfileMarker); err != nil {
		return fmt.Errorf("write gh profile marker: %w", err)
	}
	if err := writeOwnedFile(paths.HostsFile, profileData, func(data []byte) bool {
		if oldState == nil {
			return ownedHosts(data) && string(data) == string(profileData)
		}
		return string(data) == string(profileYAML(oldState.Host, oldState.Account)) || string(data) == string(profileData)
	}); err != nil {
		return fmt.Errorf("write tokenless gh profile: %w", err)
	}
	if err := writeOwnedFile(paths.ProfileConfig, []byte(profileConfig), ownedProfileConfig); err != nil {
		return fmt.Errorf("write gh profile config: %w", err)
	}
	if err := writeOwnedFile(paths.GitConfigFile, gitData, ownedGitFile); err != nil {
		return fmt.Errorf("write repository credential helper: %w", err)
	}

	if err := a.Git.ReplaceLocal(ctx, root, gitconfig.IdentityKey, []string{resolved.Account}); err != nil {
		return err
	}
	if err := a.Git.ReplaceLocal(ctx, root, gitconfig.HostKey, []string{resolved.Host}); err != nil {
		return err
	}
	if err := a.Git.ReplaceLocal(ctx, root, "user.name", []string{resolved.Name}); err != nil {
		return err
	}
	if err := a.Git.ReplaceLocal(ctx, root, "user.email", []string{resolved.Email}); err != nil {
		return err
	}
	if !containsValue(current.IncludePaths, gitconfig.IncludePath) {
		if err := a.Git.AddLocal(ctx, root, "include.path", gitconfig.IncludePath); err != nil {
			return err
		}
	}

	state := bindingState{
		Version:     stateVersion,
		Account:     resolved.Account,
		Host:        resolved.Host,
		AuthorName:  resolved.Name,
		AuthorEmail: resolved.Email,
		Snapshot:    baseline,
	}
	stateData, err := marshalState(state)
	if err != nil {
		return err
	}
	if err := writeOwnedFile(paths.StateFile, stateData, ownedState); err != nil {
		return fmt.Errorf("write gh-git state: %w", err)
	}
	committed = true

	if _, err := fmt.Fprintf(a.Out, "Bound repository to %s account %s.\n", resolved.Host, resolved.Account); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(a.Out, "Git author: %s <%s>\n", resolved.Name, resolved.Email); err != nil {
		return err
	}
	if sshAlias != "" {
		_, err = fmt.Fprintf(a.Out, "Git SSH: using existing %s alias through a repository-local URL rewrite.\n", sshAlias)
	} else {
		_, err = fmt.Fprintln(a.Out, "Git HTTPS: repository-local credential helper installed; SSH keys were not changed.")
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Out, "For direct gh commands, source `eval \"$(gh git shell-init bash)\"` once in each bash shell (or use the matching shell).")
	return err
}

func chooseHost(remotes []gitconfig.Remote, override string) (string, error) {
	if override != "" {
		host := strings.ToLower(strings.TrimSpace(override))
		if err := identity.ValidateHost(host); err != nil {
			return "", err
		}
		return host, nil
	}
	for _, remote := range remotes {
		if remote.Name == "origin" && remote.Host != "" {
			if err := identity.ValidateHost(remote.Host); err != nil {
				return "", err
			}
			return remote.Host, nil
		}
	}
	for _, remote := range remotes {
		if remote.Host != "" {
			if err := identity.ValidateHost(remote.Host); err != nil {
				return "", err
			}
			return remote.Host, nil
		}
	}
	return defaultGitHost, nil
}

func managedSnapshotMatches(current configSnapshot, state bindingState) bool {
	return valuesEqual(current.Identity, []string{state.Account}) &&
		valuesEqual(current.Host, []string{state.Host}) &&
		valuesEqual(current.UserName, []string{state.AuthorName}) &&
		valuesEqual(current.UserEmail, []string{state.AuthorEmail}) &&
		containsValue(current.IncludePaths, gitconfig.IncludePath)
}

func validateGeneratedFiles(paths gitconfig.Paths, oldState *bindingState, newProfile []byte) error {
	if data, err := os.ReadFile(paths.ProfileMarker); err == nil && !ownedProfileMarker(data) {
		return fmt.Errorf("refusing to overwrite non-gh-git profile marker %s", paths.ProfileMarker)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if data, err := os.ReadFile(paths.HostsFile); err == nil {
		allowed := string(data) == string(newProfile)
		if oldState != nil {
			allowed = allowed || string(data) == string(profileYAML(oldState.Host, oldState.Account))
		}
		if !allowed {
			return fmt.Errorf("refusing to overwrite modified gh profile %s", paths.HostsFile)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if data, err := os.ReadFile(paths.GitConfigFile); err == nil && !ownedGitFile(data) {
		return fmt.Errorf("refusing to overwrite non-gh-git file %s", paths.GitConfigFile)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if data, err := os.ReadFile(paths.ProfileConfig); err == nil && !ownedProfileConfig(data) {
		return fmt.Errorf("refusing to overwrite modified gh profile config %s", paths.ProfileConfig)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if data, err := os.ReadFile(paths.StateFile); err == nil && !ownedState(data) {
		return fmt.Errorf("refusing to overwrite malformed state %s", paths.StateFile)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func marshalState(state bindingState) ([]byte, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode gh-git state: %w", err)
	}
	return append(data, '\n'), nil
}
