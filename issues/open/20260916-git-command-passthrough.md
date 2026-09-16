# `gh git` から実際の Git コマンドを透過実行できるようにする

Status: open / implemented locally, pending commit
Model: gpt-5.6-sol
Created: 2026-09-16
Updated: 2026-09-16
Branch: main (uncommitted)

## 概要

これまでの開発作業で直接 `git` として実行してきた `status`、`diff`、`log`、`rev-parse`、`add`、`commit`、`fetch`、`pull`、`push`、`branch`、`switch`、`tag`、`worktree` などを、固定 allowlist ではなく通常の Git argv passthrough として `gh git ...` から実行できるようにする。

gh-git の既存責務であるリポジトリ単位 GitHub identity / credential wiring は維持し、`gh auth switch` を使わない。

## 背景 / 摩擦

- 開発作業では GitHub CLI 操作を `gh`、Git 操作を `git` と別々に呼ぶ必要があり、リポジトリ単位認証を提供する `gh-git` を導入しても Git 操作の入口が統一されていなかった。
- 既存の `gh git status` は gh-git 自身の binding status に使われており、Git 本来の `git status` と衝突している。
- `init`、`help`、`credential` なども Git 本来のサブコマンドまたは内部 helper と名前が重なるため、曖昧さを明示的に処理する必要がある。
- wrapper が shell を介したり exit code を 1 に畳み込むと、Git の引数、stdio、終了状態を壊し、agent/CI で利用しにくい。

## 方針

1. `gh git <git arguments...>` を原則として実際の `git` へそのまま委譲する。
2. shell は使わず argv を保持し、stdin/stdout/stderr、cwd、env、context と Git の exit code を維持する。
3. `gh git status` と `gh git init` は Git 本来のコマンドとする。
4. gh-git 自身の状態確認は `gh git binding status [--json]` へ移す。
5. `bind`、`unbind`、`accounts`、`doctor`、`env`、`shell-init` は既存互換のためトップレベル管理 alias を維持する。
6. `gh git -- <git arguments...>` を用意し、gh-git 管理コマンド名と衝突する Git alias / subcommand を明示的に実 Git へ送れるようにする。
7. `credential` は gh-git が生成した managed helper 契約だけ内部処理し、それ以外は実 `git credential ...` へ委譲する。
8. Git の破壊的フラグを gh-git が独自に禁止・確認・意味変更しない。安全性は Git 自身の契約に従う。

## 受け入れ条件

- [x] `gh git status` が実際の Git status を実行し、`gh git binding status` が gh-git binding diagnostics を実行する。
- [x] `gh git -- <args...>` で管理コマンドとの衝突を回避できる。
- [x] 任意の Git argv を shell 再解釈なしで渡せる。空白を含む path / commit message も壊れない。
- [x] stdin/stdout/stderr、cwd、env を維持する。
- [x] `git diff --exit-code` 等の非 0 exit code を top-level `gh-git` がそのまま返す。
- [x] `git add` / `commit` 等の write 操作を一時 repository で検証する。
- [x] local bare remote を使い、可能な範囲で `fetch` / `pull` / `push` の passthrough をネットワークなしで検証する。
- [x] `help` / `init` / `credential` / management command routing の回帰テストを追加する。
- [x] README、`skills/gh-git/SKILL.md`、`gh git -h` が新しい routing を同じ意味で説明する。
- [x] `gofmt`、`go test ./...`、`go vet ./...` が PASS する。
- [x] `.github/workflows/release.yml` の既存 release contract を壊さない。

## 実装・検証結果

- generic passthrough は `exec.CommandContext` で real `git` を直接起動し、shell を介さない。
- `status` / `init` は Git へ戻し、binding 診断は `binding status` に移した。
- `main` は Git subprocess の exit code をそのまま process exit code として返す。
- 空白を含む path は `status --short -z` の porcelain 契約でテストし、表示用 C-style quoting に依存しない。
- local bare remote を使った push / fetch / pull の network-free E2E を追加した。
- `go test ./...`、`go vet ./...`、`gofmt -d`、`git diff --check` はローカル gate で PASS を確認した。
- `.github/workflows/release.yml` は変更せず、release/tag push は実行していない。

## テスト上の注意

`git status --short` の path は Git porcelain の規則により空白を含む path が C-style quote される場合がある。テストは human-readable 表示の偶然の quoting に依存せず、Git の契約に沿って検証する。

## 対象外

- Git コマンドの allowlist 化
- Git の destructive operation に gh-git 独自 approval を追加すること
- `gh auth switch` による host-global account 切替
- remote URL、SSH key、GitHub token の自動変更
- 今回の変更に伴う release/tag push
