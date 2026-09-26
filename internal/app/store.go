package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/f4ah6o/gh-git/internal/store"
)

func (a *App) runStoreCommand(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, err := io.WriteString(a.Out, `gh git store — bare Git repository store primitives

Usage:
  gh git store ensure <owner/repo|repository-url> [--root <path>] [--json]
  gh git store inspect <owner/repo|repository-url> [--root <path>] [--json]
  gh git store fetch <owner/repo|repository-url> [--root <path>] [--json]

The store is a bare Git substrate. It does not create or manage jj workspaces,
task ownership, snapshots, checkpoints, writer reservations, or a generic
workspace abstraction.

--root overrides the store root for this invocation. GH_GIT_STORE_ROOT,
XDG_DATA_HOME, and then ~/.local/share/gh-git/stores define the default.
--json is accepted explicitly; store commands always emit the stable JSON schema.
`)
		return err
	}

	operation := args[0]
	if operation != "ensure" && operation != "inspect" && operation != "fetch" {
		return fmt.Errorf("unknown store command %q; run `gh git store --help`", operation)
	}
	spec, root, err := parseStoreArgs(args[1:])
	if err != nil {
		return err
	}
	repository, err := store.ParseRepository(spec)
	if err != nil {
		return err
	}
	manager := store.New(root)

	var result store.Result
	switch operation {
	case "ensure":
		result, err = manager.Ensure(ctx, repository)
	case "inspect":
		result, err = manager.Inspect(ctx, repository)
	case "fetch":
		result, err = manager.Fetch(ctx, repository)
	}
	if result.SchemaVersion != 0 {
		if writeErr := writeStoreJSON(a.Out, result); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func parseStoreArgs(args []string) (spec, root string, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			// Store commands are always structured JSON; this flag makes that intent explicit.
		case args[i] == "--root":
			if i+1 >= len(args) {
				return "", "", errors.New("--root requires a value")
			}
			i++
			root = args[i]
		case strings.HasPrefix(args[i], "--root="):
			root = strings.TrimPrefix(args[i], "--root=")
			if root == "" {
				return "", "", errors.New("--root requires a value")
			}
		case strings.HasPrefix(args[i], "-"):
			return "", "", fmt.Errorf("unknown store flag %q", args[i])
		case spec == "":
			spec = args[i]
		default:
			return "", "", errors.New("usage: gh git store <ensure|inspect|fetch> <owner/repo|repository-url> [--root <path>] [--json]")
		}
	}
	if spec == "" {
		return "", "", errors.New("usage: gh git store <ensure|inspect|fetch> <owner/repo|repository-url> [--root <path>] [--json]")
	}
	return spec, root, nil
}

func writeStoreJSON(w io.Writer, result store.Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}
