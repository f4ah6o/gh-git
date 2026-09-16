# `gh git --version` で gh-git 自身のバージョンを表示する

Status: done
Model: gpt-5.6-sol
Created: 2026-09-16
Updated: 2026-09-16
Branch: main

## 概要

`gh-git` 自身のバージョンを CLI から確認できるようにする。

`gh git` は通常の Git コマンドを可能な限りそのまま passthrough する設計であり、Git には既に `git version` が存在する。そのため新しいトップレベル管理サブコマンド `version` は予約せず、gh-git 自身のバージョン確認には global flag の `--version` を使用する。

## CLI contract

- `gh git --version` は gh-git 自身のバージョンを表示する。
- `gh git version` は予約せず、実 Git の `git version` へ passthrough する。
- `gh git -- --version` は escape hatch として実 Git の `git --version` へ passthrough する。
- `gh git --help` / `gh git -h` の既存 help routing は維持する。

## 目的

インストール済み extension のバージョン確認を、Git passthrough の名前空間を不必要に消費せずに可能にする。

## 実装方針

- バージョン文字列は一か所から取得し、release build とローカル development build の双方で安全に表示できるようにする。
- release workflow / Go build の既存構成を確認し、既存の version/tag 情報を再利用できるならそれを優先する。
- version のために `gh git version` を management command として予約しない。
- 出力はスクリプトから扱いやすい単一行とする。

## 受け入れ条件

- [x] `gh git --version` が exit code 0 で gh-git 自身のバージョンを単一行表示する。
- [x] `gh git version` が実 Git に passthrough されることをテストで固定する。
- [x] `gh git -- --version` が実 Git に passthrough されることをテストで固定する。
- [x] `--help` / `-h` と既存 management command routing に回帰がない。
- [x] release build では release/version 情報を表示できる。
- [x] release 情報が注入されないローカル build でも、誤解を招く偽の release version を表示しない。
- [x] main help / README / Skill の必要箇所に `--version` の契約を反映する。
- [x] repository の既存テストを実行し PASS を確認する。

## 完了実績

- 実装 commit: `9517002` (`feat: add gh-git version flag`)
- 公開 release: `2026.9.4`
- release tag target: `f6c665c0097b859535c44ae1daaa107801d56afb`
- release 検証: tag SHA 一致、非 draft、非 prerelease、12 platform asset 全件 uploaded / size > 0 を確認済み。
- release 相当 build で `gh-git 2026.9.4` を確認済み。

## 非目標

- `gh git version` サブコマンドの追加。
- Git 自体の version 出力形式の変更。
- version 確認に伴う network access。
