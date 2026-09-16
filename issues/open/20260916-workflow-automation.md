# gh-git でドキュメント整備から CalVer リリースまでを自動化する

Status: open
Model: unknown
Created: 2026-09-16
Updated: 2026-09-16
Branch: feat/20260916-workflow-automation

## 概要

gh-git のリポジトリ単位認証を前提に、Skill と CLI ヘルプの整備、ドキュメントの匿名化、履歴書き換え、CalVer リリースの準備・公開・検証を安全な一連の操作として実行できるようにする。

## 背景

今回の gh-git 公開では、次の作業を手動で行った。

1. README と gh-git Skill を作成し、gh git -h に基本的な導線を追加した。
2. GitHub CLI 拡張機能としてインストールできるよう、CalVer と precompiled binary を公開する GitHub Actions workflow を追加した。
3. ドキュメント中の実在ユーザー名を <github-username> に置換し、git filter-repo で main の履歴を書き換えた。
4. 履歴書き換え後に latest タグを push し、f4ah6o/calver-action が作成した CalVer tag と Release の完了を監視した。
5. workflow 更新に必要な GitHub token の workflow scope を確認し、リリース成果物を検証した。

現在の公開 workflow は .github/workflows/release.yml にあり、YYYY.MM.PATCH、Asia/Tokyo、f4ah6o/calver-action、cli/gh-extension-precompile を使用している。

## 問題

- ソースコードだけを push した状態では gh extension install が失敗するが、リリースに必要な条件を gh-git 自身が事前検査できない。
- latest tag の作成・更新、push、workflow の起動、CalVer Release の完了待ち、成果物確認を個別の Git、gh、GitHub Actions 操作に分けて実行する必要がある。
- workflow scope、contents write 権限、対象リポジトリへのアクセス権が不足していても、失敗するまで検出できない。
- ドキュメントの匿名化と公開履歴の書き換えは破壊的操作であり、対象範囲、退避、remote ref の競合、force-with-lease、書き換え結果の確認を手動で管理する必要がある。
- README、Skill、gh git -h の Usage と安全上の注意が別々に管理されるため、将来内容がずれる可能性がある。

## 目標

バインド済みの GitHub アカウントを使い、秘密情報を出力・保存せず、次の作業を gh-git の明示的なサブコマンドから再現可能にする。

- リリース設定の生成・検査
- Skill、README、CLI ヘルプの整合性検査・更新
- 対象ドキュメントの置換候補確認と履歴匿名化
- 履歴書き換え前の退避と検証済み remote 更新
- latest trigger tag の管理
- CalVer workflow の起動・監視と Release 成果物の検証
- 自動化向け JSON 出力と、失敗時の復旧手順の提示

## 対象外

- GitHub アカウントの新規作成、OAuth device login、token の発行や保存
- gh auth switch の呼び出し、またはホスト全体の active account の変更
- SSH 鍵の生成・アップロード・ ~/.ssh の変更
- 明示的に選択されていない ref、ファイル、commit author metadata の書き換え
- repository coordinate（例: f4ah6o/gh-git）や workflow の action coordinate（例: f4ah6o/calver-action）の匿名化
- GitHub Actions 以外の CI/CD サービスへの汎用対応
- GitHub の一般的な Release 管理を gh-git の責務にすること

## 提案する方針

コマンド名は実装時に確定するが、次の責務を持つサブコマンド群を追加する。

### 1. リリース設定の生成・事前検査

- release init 相当のコマンドで、CalVer format、timezone、tag trigger、permissions、concurrency、precompile action を含む workflow を生成または更新する。
- 既存の unmanaged file を上書きせず、生成物には所有マーカーまたは明確な検査可能性を持たせる。
- release check 相当のコマンドで、Go module、workflow、必要な action input、対象ブランチ、clean worktree、remote、GitHub repository access、contents write、workflow scope を検査する。
- action version は再現性のため pin し、更新時に検査結果から差分を説明できるようにする。

### 2. Skill、README、CLI ヘルプの同期

- gh git -h の Usage、Commands、Quick start、安全上の注意を Skill と README の共通情報から生成するか、少なくとも同期検査できるようにする。
- docs check 相当のコマンドで、インストール手順、CalVer Release 前提、latest tag の役割、shell hook、token 非保存の説明を検査する。
- docs sync 相当の更新は利用者が編集した unmanaged 部分を保持し、差分と更新対象を表示する。

### 3. ドキュメント匿名化と履歴書き換え

- history sanitize 相当のコマンドを dry-run 既定とし、置換 map、対象 ref、対象 path、ヒットした commit 数・ファイル数・行数を表示する。
- 既定の対象は README、Skill、その他の明示的なドキュメント拡張子とし、ソースコードや test fixture は明示指定なしに変更しない。
- repository coordinate と action coordinate などの保護語を allowlist として扱い、意図しない公開情報の破壊を防ぐ。
- apply 時は clean worktree、対象 ref の現在 SHA、remote の現在 SHA を確認し、対象範囲を限定して履歴を書き換える。
- 書き換え前にバックアップ ref と復元可能な bundle を作成し、書き換え後に旧文字列が選択 ref の到達可能なドキュメントから消えたことを検証する。
- commit author metadata やドキュメント以外の内容は、別オプションで明示されない限り変更しない。

### 4. tag、workflow、Release の公開

- release publish 相当のコマンドで、現在の検証済み commit に latest trigger tag を作成または更新する。
- remote に想定外の変更や既存 tag の競合がある場合は中断し、再実行方法を示す。tag 更新と branch 更新には force-with-lease 相当の期待値確認を使用する。
- バインド済みアカウントの認証を使い、gh auth switch は呼び出さない。
- workflow_dispatch または latest tag push のどちらを使ったか、workflow run ID、状態、失敗した step を表示する。
- CalVer tag は action の出力を正とし、既存 tag との衝突、再実行、同一日の PATCH 採番を安全に扱う。
- Release 完了後に URL、draft/prerelease 状態、対象 commit、期待する全 platform asset、asset download URL を検証する。
- install verify 相当の検査で、source-only 状態ではなく GitHub CLI 拡張機能として利用可能な成果物が存在することを確認する。

### 5. 復旧性、出力、保守性

- 破壊的な apply、tag 更新、remote push は dry-run と明示的な確認を要求する。
- remote ref が事前確認時から変わった場合は push せず、バックアップからの復旧または再確認を案内する。
- JSON 出力を提供し、token、PAT、OAuth credential、環境変数の実値、生成 profile の秘密情報を出力しない。
- 成功・失敗を安定した exit status で返し、CI や agent が workflow run と Release を後続処理できるようにする。

想定する導線は次のとおり。

~~~text
gh git bind <account>
gh git doctor --release
gh git docs check
gh git history sanitize --replace REAL_USER=github-username --refs main --dry-run
gh git history sanitize --replace REAL_USER=github-username --refs main --apply
gh git release check
gh git release publish --trigger-tag latest --wait --verify
~~~

## 受け入れ条件

- [ ] リリース設定の不足、認証 scope、権限、clean worktree、remote、Go module、workflow の不備を、公開前に秘密情報なしで検出できる。
- [ ] リリース設定を生成・検査でき、unmanaged な既存ファイルを無断で上書きしない。
- [ ] README、gh-git Skill、gh git -h の基本導線と安全上の注意の不整合を検出でき、管理対象だけを同期できる。
- [ ] ドキュメント匿名化を dry-run で確認でき、対象 ref/path、置換件数、保護語を表示できる。
- [ ] 履歴書き換え apply は明示確認、clean worktree、バックアップ、対象 ref の限定を要求し、書き換え後の検証に失敗した場合は remote を更新しない。
- [ ] remote 更新は期待する旧 SHA を確認した force-with-lease 相当で行い、remote の同時変更を検出して中断できる。
- [ ] latest tag の作成・更新から workflow run の完了待ちまでを、バインド済みアカウントで gh auth switch なしに実行できる。
- [ ] CalVer tag と Release の URL、対象 commit、draft/prerelease 状態、必要な platform asset、拡張機能としてのインストール可能性を検証できる。
- [ ] すべての出力形式で token や credential の実値を漏らさず、JSON 出力と安定した exit status を提供する。
- [ ] 失敗した履歴書き換え、tag 競合、workflow 失敗、Release 不足について、再実行またはバックアップからの復旧手順を表示する。
- [ ] go test ./...、go vet ./...、gofmt、Skill validator が成功し、主要な成功・失敗経路をテストでカバーする。
- [ ] 利用者向けの新しいコマンド、リリース手順、履歴書き換えの注意点を CHANGES.md に記録する。

## テスト計画

- go test ./... と go vet ./... を実行する。
- gofmt -d . と Skill の validator を実行する。
- 一時 Git repository と bare remote を使い、clean/dirty worktree、remote 同時更新、tag 競合、backup 作成、dry-run、rollback、対象外 path の保持をテストする。
- ドキュメントに置換対象、保護語、複数 commit の旧文字列を含む fixture を用意し、選択した ref の到達可能履歴だけが期待どおり変わることを確認する。
- GitHub API と Actions の workflow run/Release を mock または disposable repository で検証し、scope 不足、workflow failure、asset 不足、再実行を確認する。
- token を含む環境変数や credential helper の入力を与え、標準出力・標準エラー・JSON・生成ファイルに秘密情報が現れないことを確認する。
- 成功した disposable Release に対して gh extension install <owner>/<repo> 相当のインストール検証を実行する。

## リスク

- 公開履歴の書き換えと tag 更新は不可逆になり得るため、バックアップ、期待 SHA、明示確認、復旧手順が必須である。
- CalVer の日付・timezone・PATCH 採番は GitHub Actions の並行実行や再実行と競合する可能性がある。
- workflow scope と repository 権限は token の種類や GitHub の設定に依存し、gh-git から自動付与できない。
- 置換 map が広すぎると、実在ユーザー名以外の固有名詞やコード例まで変更する可能性がある。
- action の SHA pin、Go version、GitHub CLI の Release asset 命名規則が変更されると、生成・検証ロジックの保守が必要になる。
- gh extension install は対象 repository のアクセス権にも依存するため、成果物の公開と利用者の認証確認を分離して報告する必要がある。

## 変更履歴

CHANGES.md impact: yes

項目案：

- リポジトリ単位の認証を維持したまま、ドキュメント匿名化、履歴書き換え、CalVer 拡張機能 Release の準備・公開・検証を自動化するコマンドを追加する。

## 注記

- 現在の実装には bind、unbind、status、accounts、doctor、env、shell-init、credential があり、init は案内文だけを出力する。
- 既存の .github/workflows/release.yml は latest tag の push または workflow_dispatch を起点に CalVer tag と precompiled asset を作成する。
- 実装は一度にすべてを導入せず、設定検査、ドキュメント同期、履歴匿名化、Release 公開・検証の順に分割してもよい。ただし各段階で受け入れ条件と安全境界を維持する。
