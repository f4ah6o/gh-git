package shell

import (
	"fmt"
	"strings"
)

func Init(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash":
		return bash, nil
	case "zsh":
		return zsh, nil
	case "fish":
		return fish, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (use bash, zsh, or fish)", name)
	}
}

const bash = `# gh-git shell integration. Source once from ~/.bashrc.
gh_git_apply() {
  local gh_git_profile gh_git_root
  gh_git_root="$(git rev-parse --show-toplevel 2>/dev/null)" || gh_git_root=""
  gh_git_profile=""
  if [[ -n "$gh_git_root" ]]; then
    gh_git_profile="$(git rev-parse --git-path gh-git/gh-config 2>/dev/null)"
    if [[ -n "$gh_git_profile" && "$gh_git_profile" != /* ]]; then
      gh_git_profile="$gh_git_root/$gh_git_profile"
    fi
  fi
  if [[ -n "$gh_git_profile" && -f "$gh_git_profile/.gh-git-profile" ]]; then
    if [[ -z "${GH_GIT_SAVED_AUTH_ENV_SET+x}" ]]; then
      GH_GIT_SAVED_AUTH_ENV_SET=1
      if [[ -n "${GH_TOKEN+x}" ]]; then GH_GIT_SAVED_GH_TOKEN_SET=1; GH_GIT_SAVED_GH_TOKEN="$GH_TOKEN"; else GH_GIT_SAVED_GH_TOKEN_SET=0; fi
      if [[ -n "${GITHUB_TOKEN+x}" ]]; then GH_GIT_SAVED_GITHUB_TOKEN_SET=1; GH_GIT_SAVED_GITHUB_TOKEN="$GITHUB_TOKEN"; else GH_GIT_SAVED_GITHUB_TOKEN_SET=0; fi
      if [[ -n "${GH_ENTERPRISE_TOKEN+x}" ]]; then GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET=1; GH_GIT_SAVED_GH_ENTERPRISE_TOKEN="$GH_ENTERPRISE_TOKEN"; else GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET=0; fi
      if [[ -n "${GITHUB_ENTERPRISE_TOKEN+x}" ]]; then GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET=1; GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN="$GITHUB_ENTERPRISE_TOKEN"; else GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET=0; fi
      unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN
    fi
    if [[ -z "${GH_GIT_SAVED_CONFIG_DIR_SET+x}" ]]; then
      if [[ -n "${GH_CONFIG_DIR+x}" ]]; then
        export GH_GIT_SAVED_CONFIG_DIR_SET=1
        export GH_GIT_SAVED_CONFIG_DIR="$GH_CONFIG_DIR"
      else
        export GH_GIT_SAVED_CONFIG_DIR_SET=0
      fi
    fi
    export GH_CONFIG_DIR="$gh_git_profile"
  elif [[ -n "${GH_GIT_SAVED_CONFIG_DIR_SET+x}" ]]; then
    if [[ "${GH_GIT_SAVED_GH_TOKEN_SET:-0}" == 1 ]]; then export GH_TOKEN="$GH_GIT_SAVED_GH_TOKEN"; else unset GH_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GITHUB_TOKEN_SET:-0}" == 1 ]]; then export GITHUB_TOKEN="$GH_GIT_SAVED_GITHUB_TOKEN"; else unset GITHUB_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET:-0}" == 1 ]]; then export GH_ENTERPRISE_TOKEN="$GH_GIT_SAVED_GH_ENTERPRISE_TOKEN"; else unset GH_ENTERPRISE_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET:-0}" == 1 ]]; then export GITHUB_ENTERPRISE_TOKEN="$GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN"; else unset GITHUB_ENTERPRISE_TOKEN; fi
    unset GH_GIT_SAVED_AUTH_ENV_SET GH_GIT_SAVED_GH_TOKEN_SET GH_GIT_SAVED_GH_TOKEN GH_GIT_SAVED_GITHUB_TOKEN_SET GH_GIT_SAVED_GITHUB_TOKEN GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GH_ENTERPRISE_TOKEN GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN
    if [[ "$GH_GIT_SAVED_CONFIG_DIR_SET" == 1 ]]; then
      export GH_CONFIG_DIR="$GH_GIT_SAVED_CONFIG_DIR"
    else
      unset GH_CONFIG_DIR
    fi
    unset GH_GIT_SAVED_CONFIG_DIR_SET GH_GIT_SAVED_CONFIG_DIR
  fi
}
case ";${PROMPT_COMMAND-};" in
  *";gh_git_apply;"*) ;;
  *) PROMPT_COMMAND="gh_git_apply${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
gh_git_apply
`

const zsh = `# gh-git shell integration. Source once from ~/.zshrc.
gh_git_apply() {
  local gh_git_profile gh_git_root
  gh_git_root="$(git rev-parse --show-toplevel 2>/dev/null)" || gh_git_root=""
  gh_git_profile=""
  if [[ -n "$gh_git_root" ]]; then
    gh_git_profile="$(git rev-parse --git-path gh-git/gh-config 2>/dev/null)"
    if [[ -n "$gh_git_profile" && "$gh_git_profile" != /* ]]; then
      gh_git_profile="$gh_git_root/$gh_git_profile"
    fi
  fi
  if [[ -n "$gh_git_profile" && -f "$gh_git_profile/.gh-git-profile" ]]; then
    if [[ -z "${GH_GIT_SAVED_AUTH_ENV_SET+x}" ]]; then
      GH_GIT_SAVED_AUTH_ENV_SET=1
      if [[ -n "${GH_TOKEN+x}" ]]; then GH_GIT_SAVED_GH_TOKEN_SET=1; GH_GIT_SAVED_GH_TOKEN="$GH_TOKEN"; else GH_GIT_SAVED_GH_TOKEN_SET=0; fi
      if [[ -n "${GITHUB_TOKEN+x}" ]]; then GH_GIT_SAVED_GITHUB_TOKEN_SET=1; GH_GIT_SAVED_GITHUB_TOKEN="$GITHUB_TOKEN"; else GH_GIT_SAVED_GITHUB_TOKEN_SET=0; fi
      if [[ -n "${GH_ENTERPRISE_TOKEN+x}" ]]; then GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET=1; GH_GIT_SAVED_GH_ENTERPRISE_TOKEN="$GH_ENTERPRISE_TOKEN"; else GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET=0; fi
      if [[ -n "${GITHUB_ENTERPRISE_TOKEN+x}" ]]; then GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET=1; GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN="$GITHUB_ENTERPRISE_TOKEN"; else GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET=0; fi
      unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN
    fi
    if [[ -z "${GH_GIT_SAVED_CONFIG_DIR_SET+x}" ]]; then
      if [[ -n "${GH_CONFIG_DIR+x}" ]]; then
        export GH_GIT_SAVED_CONFIG_DIR_SET=1
        export GH_GIT_SAVED_CONFIG_DIR="$GH_CONFIG_DIR"
      else
        export GH_GIT_SAVED_CONFIG_DIR_SET=0
      fi
    fi
    export GH_CONFIG_DIR="$gh_git_profile"
  elif [[ -n "${GH_GIT_SAVED_CONFIG_DIR_SET+x}" ]]; then
    if [[ "${GH_GIT_SAVED_GH_TOKEN_SET:-0}" == 1 ]]; then export GH_TOKEN="$GH_GIT_SAVED_GH_TOKEN"; else unset GH_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GITHUB_TOKEN_SET:-0}" == 1 ]]; then export GITHUB_TOKEN="$GH_GIT_SAVED_GITHUB_TOKEN"; else unset GITHUB_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET:-0}" == 1 ]]; then export GH_ENTERPRISE_TOKEN="$GH_GIT_SAVED_GH_ENTERPRISE_TOKEN"; else unset GH_ENTERPRISE_TOKEN; fi
    if [[ "${GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET:-0}" == 1 ]]; then export GITHUB_ENTERPRISE_TOKEN="$GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN"; else unset GITHUB_ENTERPRISE_TOKEN; fi
    unset GH_GIT_SAVED_AUTH_ENV_SET GH_GIT_SAVED_GH_TOKEN_SET GH_GIT_SAVED_GH_TOKEN GH_GIT_SAVED_GITHUB_TOKEN_SET GH_GIT_SAVED_GITHUB_TOKEN GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GH_ENTERPRISE_TOKEN GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN
    if [[ "$GH_GIT_SAVED_CONFIG_DIR_SET" == 1 ]]; then
      export GH_CONFIG_DIR="$GH_GIT_SAVED_CONFIG_DIR"
    else
      unset GH_CONFIG_DIR
    fi
    unset GH_GIT_SAVED_CONFIG_DIR_SET GH_GIT_SAVED_CONFIG_DIR
  fi
}
autoload -Uz add-zsh-hook
add-zsh-hook -d chpwd gh_git_apply 2>/dev/null
add-zsh-hook -d precmd gh_git_apply 2>/dev/null
add-zsh-hook chpwd gh_git_apply
add-zsh-hook precmd gh_git_apply
gh_git_apply
`

const fish = `# gh-git shell integration. Source from ~/.config/fish/config.fish.
function gh_git_apply --on-variable PWD
    set -l gh_git_root (git rev-parse --show-toplevel 2>/dev/null)
    set -l gh_git_profile
    if test -n "$gh_git_root"
        set gh_git_profile (git rev-parse --git-path gh-git/gh-config 2>/dev/null)
        if test -n "$gh_git_profile"; and not string match -q '/*' -- "$gh_git_profile"
            set gh_git_profile "$gh_git_root/$gh_git_profile"
        end
    end
    if test -n "$gh_git_profile"; and test -f "$gh_git_profile/.gh-git-profile"
        if not set -q GH_GIT_SAVED_AUTH_ENV_SET
            set -g GH_GIT_SAVED_AUTH_ENV_SET 1
            if set -q GH_TOKEN; set -g GH_GIT_SAVED_GH_TOKEN_SET 1; set -g GH_GIT_SAVED_GH_TOKEN "$GH_TOKEN"; else; set -g GH_GIT_SAVED_GH_TOKEN_SET 0; end
            if set -q GITHUB_TOKEN; set -g GH_GIT_SAVED_GITHUB_TOKEN_SET 1; set -g GH_GIT_SAVED_GITHUB_TOKEN "$GITHUB_TOKEN"; else; set -g GH_GIT_SAVED_GITHUB_TOKEN_SET 0; end
            if set -q GH_ENTERPRISE_TOKEN; set -g GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET 1; set -g GH_GIT_SAVED_GH_ENTERPRISE_TOKEN "$GH_ENTERPRISE_TOKEN"; else; set -g GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET 0; end
            if set -q GITHUB_ENTERPRISE_TOKEN; set -g GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET 1; set -g GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN "$GITHUB_ENTERPRISE_TOKEN"; else; set -g GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET 0; end
            set -e GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN
        end
        if not set -q GH_GIT_SAVED_CONFIG_DIR_SET
            if set -q GH_CONFIG_DIR
                set -gx GH_GIT_SAVED_CONFIG_DIR_SET 1
                set -gx GH_GIT_SAVED_CONFIG_DIR "$GH_CONFIG_DIR"
            else
                set -gx GH_GIT_SAVED_CONFIG_DIR_SET 0
            end
        end
        set -gx GH_CONFIG_DIR "$gh_git_profile"
    else if set -q GH_GIT_SAVED_CONFIG_DIR_SET
        if test "$GH_GIT_SAVED_GH_TOKEN_SET" = 1; set -gx GH_TOKEN "$GH_GIT_SAVED_GH_TOKEN"; else; set -e GH_TOKEN; end
        if test "$GH_GIT_SAVED_GITHUB_TOKEN_SET" = 1; set -gx GITHUB_TOKEN "$GH_GIT_SAVED_GITHUB_TOKEN"; else; set -e GITHUB_TOKEN; end
        if test "$GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET" = 1; set -gx GH_ENTERPRISE_TOKEN "$GH_GIT_SAVED_GH_ENTERPRISE_TOKEN"; else; set -e GH_ENTERPRISE_TOKEN; end
        if test "$GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET" = 1; set -gx GITHUB_ENTERPRISE_TOKEN "$GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN"; else; set -e GITHUB_ENTERPRISE_TOKEN; end
        set -e GH_GIT_SAVED_AUTH_ENV_SET GH_GIT_SAVED_GH_TOKEN_SET GH_GIT_SAVED_GH_TOKEN GH_GIT_SAVED_GITHUB_TOKEN_SET GH_GIT_SAVED_GITHUB_TOKEN GH_GIT_SAVED_GH_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GH_ENTERPRISE_TOKEN GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN_SET GH_GIT_SAVED_GITHUB_ENTERPRISE_TOKEN
        if test "$GH_GIT_SAVED_CONFIG_DIR_SET" = 1
            set -gx GH_CONFIG_DIR "$GH_GIT_SAVED_CONFIG_DIR"
        else
            set -e GH_CONFIG_DIR
        end
        set -e GH_GIT_SAVED_CONFIG_DIR_SET GH_GIT_SAVED_CONFIG_DIR
    end
end
gh_git_apply
`
