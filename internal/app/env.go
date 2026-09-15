package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (a *App) Env(ctx context.Context, shellName string) error {
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
	binding, err := loadBinding(ctx, a.Git, root)
	if err != nil {
		return err
	}
	if binding == nil {
		return errors.New("repository is not bound; run `gh git bind <account>`")
	}
	markerData, markerErr := os.ReadFile(paths.ProfileMarker)
	hostsData, hostsErr := os.ReadFile(paths.HostsFile)
	if markerErr != nil || !ownedProfileMarker(markerData) || hostsErr != nil || !profileTokenless(hostsData) {
		return errors.New("tokenless gh profile is unavailable; bind the repository again")
	}
	if shellName != "" {
		switch strings.ToLower(shellName) {
		case "bash", "zsh":
			_, err = fmt.Fprintf(a.Out, "export GH_CONFIG_DIR=%s\n", shellQuote(paths.ProfileDir))
		case "fish":
			_, err = fmt.Fprintf(a.Out, "set -gx GH_CONFIG_DIR %s\n", shellQuote(paths.ProfileDir))
		default:
			return fmt.Errorf("unsupported shell %q (use bash, zsh, or fish)", shellName)
		}
		return err
	}
	_, err = fmt.Fprintf(a.Out, "identity=%s\nhost=%s\nGH_CONFIG_DIR=%s\n", binding.Account, binding.Host, paths.ProfileDir)
	return err
}
