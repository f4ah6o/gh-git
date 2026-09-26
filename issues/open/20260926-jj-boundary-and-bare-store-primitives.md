# gh-git と Jujutsu の責務を分離し、bare Git store primitive を整備する

Status: open
Model: unknown
Created: 2026-09-26
Updated: 2026-09-26
Branch: TBD

## 概要

gh-git に Jujutsu (jj) の transaction / snapshot / recovery semantics を取り込まず、次の境界を固定する。

- **gh-git**: GitHub identity と Git repository / remote / bare store / Git worktree の primitive
- **jj**: working-copy change、snapshot、change_id、operation log、checkpoint / recovery、jj workspace
- **上位 orchestrator**: task / execution / workspace ownership、writer reservation、VCS backend 選択、snapshot timing、verification / delivery correlation

目的は、gh-git を Git / GitHub substrate として再利用可能なまま保ちつつ、jj-first な managed workspace からも Git-only compatibility backend からも同じ repository store を利用できるようにすることである。

## 背景

gh-git は現在、repository-scoped GitHub identity と Git passthrough を提供している。

既に linked worktree でも tokenless `GH_CONFIG_DIR` profile を common Git dir から解決するよう修正されており、repository と linked worktree が同じ GitHub identity を共有できる。

一方、上位の development harness では次の問題がある。

- coding agent が file を変更しても Git commit を作らないことがある
- 未commit変更が複数 task / agent 間で混ざると回収しづらい
- task / execution と filesystem change の対応を Git commit discipline だけに依存したくない
- 新規 managed repository では local main checkout を維持せず、remote-tracking ref を基準にしたい
- jj-first backend と Git compatibility backend の両方から同じ GitHub identity / remote repository substrate を使いたい

このうち snapshot / recovery 問題を gh-git が独自 Git commit broker として解決すると、staging、untracked policy、hidden commit、rebase / reset、concurrent mutation などを gh-git が抱えることになる。

その責務は持たせない。

## Fixed responsibility boundary

### gh-git

gh-git は以下を担当する。

1. **Repository-scoped GitHub identity**
   - `github.identity`
   - `github.host`
   - `user.name`
   - `user.email`
   - common Git dir 配下の tokenless `GH_CONFIG_DIR` profile
   - global `gh auth switch` に依存しない identity resolution

2. **Repository identity**
   - `host / owner / repo` を区別する
   - basename だけで repository identity を決めない
   - HTTPS / SSH / `owner/repo` を canonical repository identity へ normalize する

3. **Bare Git repository store**
   - bare repository の ensure / inspect
   - origin URL / fetch refspec の検証
   - remote-tracking refs の fetch
   - default branch の観測
   - fetch の成功時刻と観測 commit の返却
   - local `refs/heads/main` を作成・維持することを前提にしない

4. **Git compatibility primitive**
   - 必要な場合の Git worktree 作成 / inspection / removal
   - dirty / untracked / ahead / diverged 等の Git state の観測
   - unmanaged / legacy checkout を破壊せず分類する
   - caller が指定した commit / ref を materialize する低レベル操作

5. **GitHub delivery substrate**
   - repository-scoped identity を使った Git / `gh` 実行
   - push / PR 等の上位 adapter が必要とする repository / credential context

### jj

jj は gh-git の責務ではない。

jj adapter / consumer は以下を担当する。

- jj repository / workspace
- working-copy commit
- `change_id`
- jj operation id / operation log
- automatic / explicit snapshot
- checkpoint
- undo / recovery
- task の logical change の継続
- agent が Git commit を作らなくても recoverable な VCS state を保つこと
- delivery boundary で Git-compatible refs / commits へ接続すること

gh-git は jj の executable、version、config、workspace metadata、operation log を管理しない。

### 上位 orchestrator

gh-git も jj も、task ownership を決定しない。

上位 orchestrator が以下を担当する。

- task / execution / workspace ID の発行
- writer reservation / concurrency policy
- Git backend と jj backend の選択
- repository freshness policy
- task / execution start・stop・reconcile 時の snapshot timing
- `Task -> Workspace -> VCS change` の correlation
- verification 対象 revision の固定
- delivery branch / bookmark / PR との対応
- cleanup / retention policy
- unsupported repository feature で Git backend へ fallback するかどうかの判断

## Target architecture

~~~text
                       orchestrator
                           |
              +------------+------------+
              |                         |
         jj VCS adapter          Git VCS adapter
              |                         |
        jj workspace             git worktree
              |                         |
              +------------+------------+
                           |
                     gh-git substrate
                           |
        +------------------+------------------+
        |                  |                  |
 GitHub identity      bare Git store     remote/fetch facts
        |                  |                  |
        +------------------+------------------+
                           |
                         origin
~~~

重要なのは、**jj が gh-git を置き換えるのではなく、gh-git の repository substrate の上で jj-first VCS semantics を利用できること**である。

## CLI / API direction

### Store namespace

gh-git 側には repository store primitive を追加する。

想定例:

~~~text
gh git store ensure <owner/repo> --json
gh git store inspect <owner/repo> --json
gh git store fetch <owner/repo> --json
~~~

最低限返す情報:

~~~json
{
  "schema_version": 1,
  "repository": {
    "host": "github.com",
    "owner": "f4ah6o",
    "name": "example"
  },
  "store": {
    "layout": "bare",
    "path": "/.../github.com/f4ah6o/example.git"
  },
  "remote": {
    "url": "https://github.com/f4ah6o/example.git",
    "default_branch": "main"
  },
  "observations": {
    "refs/remotes/origin/main": {
      "commit": "<sha>",
      "observed_at": "<timestamp>"
    }
  },
  "fetch": {
    "last_attempt_at": "<timestamp>",
    "last_success_at": "<timestamp>",
    "last_error": null
  }
}
~~~

fetch failure を fresh と扱わない。
最後に成功した remote observation と今回の fetch attempt は分けて返す。

### `workspace` namespace は避ける

jj-first 設計では `workspace` という語が jj workspace と衝突する。

したがって gh-git に generic な `gh git workspace ...` namespace を持たせて、それを managed workspace の authoritative abstraction にしない。

Git compatibility primitive が必要なら、意味を限定した namespace にする。

例:

~~~text
gh git worktree ensure ...
gh git worktree inspect ...
gh git worktree remove ...
~~~

または内部 API のみとし、公開 CLI surface は実装 packet で決める。

`worktree` は **Git worktree を操作する primitive** であり、task workspace や jj workspace を意味しない。

## Bare store contract

### Repository identity

repository identity は最低限:

~~~json
{
  "host": "github.com",
  "owner": "f4ah6o",
  "name": "example"
}
~~~

とする。

新規 store では `repo` basename だけを identity にしない。

### Layout

default path の詳細は実装時に既存 config と整合させるが、論理 layout は次の形にする。

~~~text
<store-root>/<host>/<owner>/<repo>.git
~~~

bare store は working checkout を持たない。

remote-tracking branch を authoritative observation とし、新規 managed flow のために local main checkout / local main branch の維持を要求しない。

### Fetch

fetch は明示 operation とする。

- ensure と fetch の network side effect を分離できる API を用意する
- fetch 成功時だけ remote observation を更新する
- fetch failure 時に以前の成功 observation を消さない
- ただし以前の observation を「現在 fresh」と偽らない
- prune 後に upstream で削除された remote-tracking ref を fresh な base として残さない
- authentication error / network error / remote error を secret なしで区別可能にする

freshness threshold を gh-git 自身が policy として決めない。
gh-git は observed fact を返し、許容 freshness は caller が決める。

## Git worktree compatibility contract

Git worktree support は残すが、jj-first managed workspace の default semantics にはしない。

必要な operation:

- specified commit / ref から Git worktree を作る
- canonical path を返す
- attached / detached state を返す
- branch / HEAD を返す
- dirty / untracked を観測する
- ahead / behind / diverged を可能な範囲で観測する
- remove 前に user work の存在を fail closed で検出する

禁止事項:

- dirty worktree の自動 reset
- user worktree の自動 adopt
- unmanaged path の削除
- automatic stash
- hidden snapshot commit の作成
- task の都合による force checkout
- jj workspace を Git worktree として管理すること

## jj interoperability requirements

gh-git 自体は jj を実行しないが、consumer が jj backend を構成できるよう次を保証する。

1. bare store の canonical path を安定して取得できる。
2. origin URL と remote-tracking ref を取得できる。
3. base とする commit SHA を明示的に取得できる。
4. repository-scoped GitHub identity が linked / derived working directory でも解決できる。
5. gh-git registry に jj 固有 state を要求しない。
6. Git-only consumer と jj consumer が同じ bare store を参照しても、gh-git が working-copy semantics を勝手に変更しない。

同時 mutation の排他は gh-git が task reservation として実装せず、上位 orchestrator が所有する。

## Legacy checkout protection

既存 checkout は新規 bare-store standard と区別する。

inspect では少なくとも以下を分類できるようにする。

- managed bare store
- Git worktree
- primary / conventional checkout
- external / unmanaged checkout

既存 checkout に対して次を自動実行しない。

- move
- reset
- clean
- stash
- branch rewrite
- jj repository への変換
- delete

dirty / ahead / diverged 状態は観測結果として返す。

## Idempotency

automation / agent から安全に再試行できるよう、ensure operation は idempotent にする。

### Store ensure

同じ repository identity + compatible remote config:

- existing store を再利用
- destructive re-init をしない

異なる remote / incompatible state:

- conflict として失敗
-既存 refs や config を上書きしない

### Git worktree ensure

公開する場合は caller が stable ID を与えられるようにする。

同じ ID / repository / base / branch:

- existing worktree を再利用可能

同じ ID で異なる request:

- conflict
- existing worktree を変更しない

idempotency record は Git / filesystem から導出できる事実と照合し、registry の JSON だけを authoritative state にしない。

## Error direction

machine-readable output では stable error code を持たせる。

候補:

- `owner_required`
- `repo_ambiguous`
- `store_missing`
- `store_conflict`
- `fetch_failed`
- `base_unverified`
- `base_unresolved`
- `path_escape`
- `path_occupied`
- `worktree_conflict`
- `dirty_worktree`
- `legacy_layout`
- `schema_version_unsupported`

エラーに token、credential helper の結果、Authorization header 等を含めない。

## 実装順序

### Phase 1 — store contract

- repository identity parser / canonicalizer
- store root / canonical path
- bare store ensure / inspect
- origin / refspec verification
- JSON schema
- path escape protection

### Phase 2 — fetch / observation

- explicit fetch
- remote default branch observation
- remote-tracking SHA observation
- timestamps
- prune
- structured failure
- secret redaction

### Phase 3 — Git worktree compatibility primitive

jj backend の実装とは独立して必要性を確認する。

実装する場合:

- ensure / inspect
- idempotency
- dirty protection
- remove safety
- legacy classification

generic task `workspace` abstractionは追加しない。

### Phase 4 — consumer acceptance

disposable bare remote を使い、少なくとも次を検証する。

- Git-only consumer が bare store + Git worktree を利用できる
- jj consumer が同じ repository identity / bare store / observed base commit を利用できる
- gh-git 自身は jj metadata を持たない
- worktree / jj workspace の lifecycle が gh-git の task ownership として混ざっていない

## 受け入れ条件

- [ ] repository identity が host / owner / repo で一意に表現される。
- [ ] bare store を ensure / inspect できる。
- [ ] bare store の標準利用で local main checkout を必要としない。
- [ ] fetch 成功時の remote-tracking SHA と観測時刻を取得できる。
- [ ] fetch failure を最新確認済みとして扱わない。
- [ ] repository-scoped GitHub identity が bare store と derived working directory で利用できる。
- [ ] gh-git に jj executable / jj workspace / change_id / operation log / checkpoint / undo の管理を追加していない。
- [ ] gh-git に task / execution ownership、writer reservation、snapshot scheduling を追加していない。
- [ ] generic `workspace` namespace を Git worktree と jj workspace の共通 abstraction として追加していない。
- [ ] Git worktree primitive を提供する場合、その名称と schema から Git-specific operation であることが明確。
- [ ] dirty / ahead / diverged な legacy checkout を inspect しても内容・refs を変更しない。
- [ ] worktree removal は uncommitted / untracked user work を既定で破棄しない。
- [ ] ensure の再送で repository / worktree を重複作成しない。
- [ ] incompatible な同一 ID / repository state は既存状態を上書きせず conflict になる。
- [ ] JSON / stderr / logs に GitHub token や credential を出力しない。
- [ ] Git-only consumer と jj consumer の fixture acceptance を分けて検証する。
- [ ] `go test ./...`、`go vet ./...`、gofmt check が成功する。

## テスト計画

### Unit

- repository identity normalization
- owner 違い同名 repository
- path escape
- store request fingerprint / conflict
- JSON schema
- error code
- credential redaction

### Integration — local Git remote

temporary bare origin を作成して検証する。

1. main commit を remote に用意する。
2. gh-git store を新規 ensure する。
3. local `refs/heads/main` を必要としないことを確認する。
4. fetch 後の remote-tracking SHA / timestamp を確認する。
5. upstream main を進めて再 fetch し、observation が更新されることを確認する。
6. remote を取得不能にし、last successful observation と failed attempt が区別されることを確認する。
7. branch delete + prune を行い、削除 ref が fresh observation から消えることを確認する。

### Integration — Git worktree

実装する場合:

- same request reuse
- same ID / different base conflict
- dirty file
- untracked file
- ahead commit
- diverged branch
- safe remove refusal
- unmanaged occupied path

### Consumer contract — jj

jj が利用可能な test environment では、gh-git の CLI から jj を起動するのではなく consumer-side fixture として検証する。

- gh-git store path / observed base SHA を取得できる
- consumer がその Git backend を使った jj repository / workspace を構成できる
- change / snapshot 後も gh-git store inspect が repository fact を返せる
- gh-git registry に jj change_id / operation id を保存していない

jj が test host に無い場合、この gate は **未実行** と報告し、Git tests の PASS を jj acceptance PASS と読み替えない。

## 対象外

- jj の install / version management
- jj command wrapper
- jj snapshot / checkpoint / undo
- task / execution records
- writer reservation
- orchestration
- environment setup
- verification state
- PR / stacked PR orchestration
- gh-stack の lifecycle 管理
- agent backend 選択
- Git staging / hidden snapshot commit broker

## 既存実装との互換性

- `gh git <git args>` passthrough は維持する。
- `bind` / `unbind` / `binding status` / `accounts` / `doctor` / `env` / `shell-init` の意味を変えない。
- common Git dir を基準にした linked-worktree profile resolution を維持する。
- 新しい store primitive を導入するために既存 repository を自動 migration しない。

## 関連

- `f4ah6o/temote-mcp/issues/open/20260924-temote-development-harness-restructure.md`
- `f4ah6o/temote-mcp/issues/open/20260925-f1-repository-store-workspace-contract.md`
- `f4ah6o/temote-mcp/issues/open/20260925-vcs-transaction-jj-first.md`
- `f4ah6o/temote-mcp/issues/open/20260925-v2-vcs-workspace-contract.md`

## 変更履歴

CHANGES.md impact: yes

項目案:

- jj-first / Git compatibility の両方から利用できる bare repository store primitive を追加し、gh-git の責務を GitHub identity / Git substrate に限定する。
