package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/f4ah6o/gh-git/internal/gitconfig"
)

func (a *App) Unbind(ctx context.Context) error {
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
	state, err := loadState(paths.StateFile)
	if err != nil {
		return err
	}
	if state == nil {
		return errors.New("no gh-git binding state found; refusing to guess which local author values to remove")
	}
	current, err := captureSnapshot(ctx, a.Git, root)
	if err != nil {
		return err
	}
	warnings := make([]string, 0)

	managed := configSnapshot{
		Identity: []string{state.Account}, Host: []string{state.Host},
		UserName: []string{state.AuthorName}, UserEmail: []string{state.AuthorEmail},
	}
	if valuesEqual(current.Identity, managed.Identity) {
		if err := a.Git.ReplaceLocal(ctx, root, gitconfig.IdentityKey, state.Snapshot.Identity); err != nil {
			return err
		}
	} else {
		warnings = append(warnings, "github.identity was changed after bind; it was left untouched")
	}
	if valuesEqual(current.Host, managed.Host) {
		if err := a.Git.ReplaceLocal(ctx, root, gitconfig.HostKey, state.Snapshot.Host); err != nil {
			return err
		}
	} else {
		warnings = append(warnings, "github.host was changed after bind; it was left untouched")
	}
	if valuesEqual(current.UserName, managed.UserName) {
		if err := a.Git.ReplaceLocal(ctx, root, "user.name", state.Snapshot.UserName); err != nil {
			return err
		}
	} else {
		warnings = append(warnings, "user.name was changed after bind; it was left untouched")
	}
	if valuesEqual(current.UserEmail, managed.UserEmail) {
		if err := a.Git.ReplaceLocal(ctx, root, "user.email", state.Snapshot.UserEmail); err != nil {
			return err
		}
	} else {
		warnings = append(warnings, "user.email was changed after bind; it was left untouched")
	}

	if containsValue(current.IncludePaths, gitconfig.IncludePath) {
		if err := a.Git.UnsetMatchingLocal(ctx, root, "include.path", "^"+gitconfig.IncludePath+"$"); err != nil {
			return err
		}
	}

	if removed, removeErr := removeOwnedFile(paths.GitConfigFile, ownedGitFile); removeErr != nil {
		return removeErr
	} else if !removed {
		warnings = append(warnings, "generated Git config was modified; it was preserved")
	}
	if removed, removeErr := removeOwnedFile(paths.HostsFile, func(data []byte) bool {
		return string(data) == string(profileYAML(state.Host, state.Account))
	}); removeErr != nil {
		return removeErr
	} else if !removed {
		if _, statErr := os.Stat(paths.HostsFile); statErr == nil {
			warnings = append(warnings, "gh profile was modified; it was preserved without its shell marker")
		}
	}
	if _, removeErr := removeOwnedFile(paths.ProfileConfig, ownedProfileConfig); removeErr != nil {
		return removeErr
	}
	if _, removeErr := removeOwnedFile(paths.ProfileMarker, ownedProfileMarker); removeErr != nil {
		return removeErr
	}
	if err := removeStateFile(paths.StateFile, state); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(a.Out, "Removed gh-git repository binding."); err != nil {
		return err
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(a.Out, "Warning: %s.\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func removeStateFile(path string, expected *bindingState) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var actual bindingState
	if err := decodeState(data, &actual); err != nil || actual.Version != stateVersion || actual.Account != expected.Account || actual.Host != expected.Host {
		return fmt.Errorf("refusing to remove changed gh-git state %s", path)
	}
	return os.Remove(path)
}
