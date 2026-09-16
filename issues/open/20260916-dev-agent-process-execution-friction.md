# Temote/Codex 開発経路で subprocess と Go cache が利用できない場合の事前検査を追加する

Status: open
Model: gpt-5.6-sol
Created: 2026-09-16
Updated: 2026-09-16
Branch: main

## 概要

gh-git の passthrough 実装中、Temote セッション自体の `execute` は利用できる一方、`local_agent_run` から起動した Codex は最初の `pwd` を含む全 subprocess 生成が `CreateProcess: Operation not permitted (os error 1)` で失敗した。また、その後の `codex_status` は `user denied Codex operation` を返した。

同じ作業で通常の `go test ./...` は `$HOME/.cache/go-build` が read-only のため setup failed になり、repository 内の writable `GOCACHE` を明示すると正常にテストできた。
さらに `go build` は成功したものの、既定の module cache 配下への stat cache 書き込みで `read-only file system` warning が出たため、Go 開発 preflight は `GOCACHE` だけでなく `GOMODCACHE` の writable 可否も扱う必要がある。

これらはソース不具合ではなく開発実行経路の摩擦だが、現在は実装開始後に初めて判明する。

## 観測した事実

1. `local_agent_run` の Codex turn は開始・終了したが、`/bin/bash -lc ...`、`/bin/bash -c pwd`、`/bin/sh -c pwd`、`/usr/bin/env` の process launch がすべて `Operation not permitted` で拒否された。
2. 同一 Temote session の `execute` では `git status`、`gofmt`、`go test` 等を実行できた。
3. `codex_status` は `user denied Codex operation` を返したため、Codex 経路の再試行は行わず Temote 直接編集へ切り替えた。
4. `go test ./...` は `/home/hirohito-fujita/.cache/go-build/...: read-only file system` で失敗した。
5. `GOCACHE=<repo>/.tmp/go-build go test ./...` では全 package を実行できた。
6. `GOCACHE` を writable にしても `go build` が `$HOME/go/pkg/mod/cache/download/...tmp: read-only file system` warning を出すケースがあった。

## 問題

- Agent を使えるかどうかを作業開始前に判定できず、実装担当の切替が遅れる。
- Go の標準 build/module cache path が sandbox から writable かどうかを事前に確認していない。
- Temote 本体と child agent で process execution capability が異なる場合、その差分が利用者から見えにくい。
- 一時的な child-agent failure と approval denial が同じ「Codex が使えない」に見えやすい。

## 目標

開発タスク開始時に、必要な execution capability と language-specific writable cache を短時間で preflight し、利用不能な経路を実装開始前に切り分けられるようにする。

## 受け入れ条件

- [ ] Agent 実行前に child subprocess の最小 probe を行い、process creation 不可を明示的に検出できる。
- [ ] approval denial と sandbox/process capability failure を別状態として報告する。
- [ ] Go project では `go env GOCACHE GOMODCACHE` の writable 可否を確認し、sandbox 内 writable cache を安全に選べる。
- [ ] fallback した場合も「agent 未使用」「Temote direct execution 使用」を結果に残せる。
- [ ] preflight が source tree を変更せず、秘密情報を出力しない。

## 回避策

今回の作業では Temote の `read_file` / `apply_patch` / `execute` に切り替え、Go test は repository 内 `.tmp/go-build` を `GOCACHE` に指定した。module cache warning は build 自体を失敗させなかったが、今後は writable `GOMODCACHE` も preflight で決定する。
