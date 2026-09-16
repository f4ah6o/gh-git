---
name: gh-git
description: "gh git 経由の Git passthrough と、gh-git のリポジトリ単位 GitHub アカウント紐付け、Git author、認証、shell hook、HTTPS/SSH の使い方を説明・診断する。ユーザーが gh git の Git コマンド実行、bind、unbind、binding status、accounts、doctor、env、shell-init、credential、または gh git -h のヘルプを尋ねたときに使う。"
---

# gh-git

## 目的

`gh-git` は、通常の Git コマンドを `gh git ...` で実行しつつ、リポジトリごとに GitHub アカウントを選択する `gh` 拡張機能である。
Git の author と GitHub CLI の認証プロファイルをリポジトリ単位で切り替え、ホスト全体の `gh auth switch` を変更しない。

使い方を説明するときは、まず `gh git -h` を実行して現在の CLI に組み込まれた短いリファレンスを確認し、必要に応じてこの手順とリポジトリ直下の `README.md` を参照する。

## 最短手順

リポジトリで次を実行する。

```bash
# 各シェルで一度だけ実行する（bash の例）
eval "$(gh git shell-init bash)"

cd /path/to/repository
gh git bind <github-username>
gh git binding status
gh git status
```

`<github-username>` には `gh auth status` で確認できる GitHub ログイン名を指定する。
GitHub Enterprise など別ホストを使うときは `--hostname <host>` を指定する。

```bash
gh git bind <github-username> --hostname github.example.com
```

未インストールなら、公開済み拡張機能を次でインストールする。

```bash
gh extension install f4ah6o/gh-git
```

リモートインストールには、対象 OS/CPU 用のバイナリを添付した CalVer Release が必要である。
ソースだけを push した直後に `extension is not installable: no usable artifact or script found` が出る場合は、Release が未作成か、リポジトリ直下に実行可能な `gh-git` script がない。
このリポジトリでは `latest` tag の push または `workflow_dispatch` で `.github/workflows/release.yml` を実行し、`YYYY.MM.PATCH`（Asia/Tokyo）の CalVer tag と Release artifact を公開する。

開発中のリポジトリでは `go build -o gh-git .` でビルドし、生成した `gh-git` を使う。

## コマンド

| コマンド | 用途 |
| --- | --- |
| `gh git <git arguments...>` | 引数を shell で再解釈せず、実際の `git` へ委譲する。stdio、cwd、env、exit code を維持する |
| `gh git -- <git arguments...>` | gh-git 側のコマンド名との曖昧さを避けて Git に明示委譲する |
| `gh git bind <github-username> [--hostname <host>]` | 現在のリポジトリを GitHub アカウントへ紐付け、Git author と認証用設定を生成する |
| `gh git unbind` | 紐付けを解除し、最初の bind 前に保存したローカル author を復元する |
| `gh git binding status [--json]` | 紐付け、author、Git 配線、プロファイル、資格情報の利用可否を秘密情報なしで表示する |
| `gh git accounts [--hostname <host>]` | 保存済みアカウントを表示する。アクティブアカウントは変更しない |
| `gh git doctor` | 紐付けに不足・危険な設定がないか検査する |
| `gh git env [--shell <bash\|zsh\|fish>]` | 現在のリポジトリ用の tokenless `GH_CONFIG_DIR` を表示する |
| `gh git shell-init <bash\|zsh\|fish>` | ディレクトリ移動時にプロファイルを選ぶ shell hook を出力する |

`credential --managed` は Git の repository-local credential helper から呼ばれる内部コマンドであり、通常は直接実行しない。それ以外の `gh git credential ...` は実 Git に委譲する。

`status` と `init` は Git 本来のサブコマンドとして扱う。gh-git 自身の状態確認は `gh git binding status` を使う。
`bind`、`unbind`、`accounts`、`doctor`、`env`、`shell-init` は互換性のためトップレベル管理コマンドとして維持する。

代表例:

```bash
gh git status
gh git fetch --prune
gh git pull --ff-only
gh git add -- path/to/file
gh git commit -m "message"
gh git push
gh git diff --stat
gh git log --oneline -10
gh git show HEAD
gh git branch -vv
gh git switch -c feature/example
gh git checkout -- path/to/file
gh git merge topic
gh git rebase main
gh git tag --list
gh git worktree list
gh git remote -v
gh git rev-parse HEAD
gh git ls-files
gh git grep pattern
gh git restore path/to/file
gh git reset HEAD -- path/to/file
```

passthrough は allowlist ではない。`git` が受け付ける破壊的フラグもそのまま実行されるため、gh-git が追加確認や意味変更を行うと説明してはならない。

## `gh` と `git` の選択範囲

`gh git bind` は次の設定をリポジトリの `.git` 配下に生成する。

- `github.identity` と `github.host`
- リポジトリ単位の `user.name` と `user.email`
- tokenless な `GH_CONFIG_DIR` プロファイル
- `gh git credential --managed` を使う Git 設定

Git の HTTPS 操作（`git fetch`、`git pull`、`git push` など）は、shell hook なしでも repository-local helper を利用する。

一方、`gh pr create`、`gh issue create`、`gh api user` などの直接の `gh` コマンドは、別プロセスとして起動するため shell hook が必要である。
hook を常駐させたくない agent や一回限りの実行では、次のように環境を設定してからコマンドを実行する。

```bash
eval "$(gh git env --shell bash)"
gh pr create
```

`gh git env` はプロファイルの場所だけを設定し、token を出力・生成しない。

## shell hook

対応 shell は `bash`、`zsh`、`fish` である。

```bash
eval "$(gh git shell-init bash)"   # bash
eval "$(gh git shell-init zsh)"    # zsh
gh git shell-init fish | source    # fish
```

hook は bind 済みリポジトリに入ると `GH_CONFIG_DIR` をそのリポジトリのプロファイルへ変更し、外へ出ると元に戻す。
GitHub CLI が token 環境変数を優先するため、hook は `GH_TOKEN`、`GITHUB_TOKEN`、`GH_ENTERPRISE_TOKEN`、`GITHUB_ENTERPRISE_TOKEN` を一時的に退避・解除し、リポジトリの外で復元する。
token の値をリポジトリへ書き込むことはない。

## 認証と SSH の注意

- `gh auth switch` は呼び出さない。別リポジトリや別ターミナルのアクティブアカウントを変更しない。
- token、PAT、OAuth credential、password は `.git/config`、生成プロファイル、追跡対象ファイルへ保存しない。
- Git HTTPS は `gh auth token --hostname <host> --user <account>` を内部で使い、token をメモリ上で Git の credential protocol に渡す。
- GitHub CLI が OS keyring からアカウント別 credential を解決できない環境では、`gh git doctor` と `gh auth status` の結果を確認する。CI では短命の `GH_TOKEN` または `GITHUB_TOKEN` を通常の方法で注入する。
- SSH の鍵生成、アップロード、`~/.ssh` の変更、remote URL の変更は行わない。
- SSH remote をアカウント単位で選択するには、あらかじめ `github-<github-username>` という既存 alias を用意する。alias がなければ `binding status`/`doctor` の警告に従い、意図しない鍵で接続しない。

例:

```sshconfig
Host github-<github-username>
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519_<github-username>
    IdentitiesOnly yes
```

## 診断の順序

問題が起きたら、次の順序で確認する。

1. `gh git binding status` で対象アカウント、host、author、Git HTTPS/SSH 配線を確認する。
2. `gh git binding status --json` で機械的に結果を確認する。
3. `gh git doctor` で不足・危険な構成を確認する。
4. `gh auth status --hostname <host>` で対象アカウントの credential が利用可能か確認する。
5. 直接の `gh` コマンドだけが失敗する場合は、現在の shell で `eval "$(gh git shell-init bash)"`（shell に合わせて変更）を実行したか確認する。
6. bind をやり直す前に、既存の管理設定を `gh git unbind` で安全に解除する。

`unbind` は bind 後に利用者が変更した managed value を勝手に上書きしない。
変更された値は残し、警告を表示する。

## 説明時の要点

- 「アカウントを切り替える」は、ホスト全体の `gh auth switch` ではなく `gh git bind <github-username>` を指すと説明する。
- `gh git -h` にはこの Skill と同じ基本導線（Usage、Commands、Quick start、安全上の注意）が表示されると説明する。
- token の実値、環境変数の内容、生成ファイルの credential を出力例に含めない。
- repository config は untrusted input として扱い、信頼できない worktree の bind や生成ファイルを無検証で受け入れない。
