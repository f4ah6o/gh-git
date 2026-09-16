# gh-git 管理コマンドと Git passthrough の名前衝突ポリシーを固定する

Status: open
Model: gpt-5.6-sol
Created: 2026-09-16
Updated: 2026-09-16
Branch: main

## 概要

`gh git <git arguments...>` を generic passthrough にすると、gh-git 自身の管理コマンドと Git のサブコマンド名が同じ場合に routing が衝突する。

今回すでに実在した衝突は `status` と `init` である。従来の `gh git status` は gh-git の binding 診断、`gh git init` は gh-git の案内だったが、Git 互換 UX を優先して両方を実 Git に戻し、binding 診断を `gh git binding status` へ移した。

## 現在の方針

- `gh git status` / `gh git init` は実 Git に passthrough する。
- gh-git の状態確認は `gh git binding status [--json]` を使う。
- `bind`、`unbind`、`accounts`、`doctor`、`env`、`shell-init` は既存利用者向けトップレベル alias として予約する。
- 内部 credential helper の `credential --managed <get|store|erase>` は gh-git が処理する。それ以外の `credential ...` は Git に渡す。
- `gh git -- <git arguments...>` は予約名を含めて Git に強制委譲する escape hatch とする。
- `gh git -h` / `gh git --help` と引数なし `gh git help` は extension help を表示する。Git 側の global help が必要なら `gh git -- --help` を使う。

## 摩擦

- generic passthrough と言っても、トップレベル alias を維持する限り 100% transparent ではない。
- Git が将来同名の公式サブコマンドを追加した場合、既存 alias と再び衝突する可能性がある。
- `git-<name>` 形式の利用者独自 external subcommand と管理 alias が衝突する可能性がある。
- 後方互換 alias をいつまで維持するかが未定義だと、routing 仕様が増殖する。

## 目標

passthrough と管理 namespace の優先順位を versioned CLI contract として固定し、将来のコマンド追加時に互換性を機械的に検査できるようにする。

## 受け入れ条件

- [ ] reserved management names と passthrough precedence を一か所で定義する。
- [ ] Git 標準 subcommand / global option と reserved names の collision test を持つ。
- [ ] external `git-<name>` subcommand を含む collision test を持つ。
- [ ] `--` escape hatch は全 reserved name に対して実 Git へ委譲できる。
- [ ] 新しい管理コマンドは原則 `binding` 等の management namespace 配下へ追加し、トップレベル予約名を増やさない。
- [ ] 将来トップレベル alias を廃止する場合の deprecation policy を README/help/CHANGES に定義する。

## 注記

今回の passthrough 実装では上記の現在方針まで実装・テスト・文書化する。alias の長期 deprecation policy と自動 collision detection は follow-up とする。
