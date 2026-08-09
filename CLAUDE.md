# CLAUDE.md

このファイルは、このリポジトリで作業する際にClaude Codeが常に読み込む前提知識です。

## このプロジェクトは何か

Weathernews(WNI)の有料緊急地震速報(EEW)配信サービス「FastCaster」をTCPで受信し、Slack/IRCへ通知するボット。元はPerl実装(`irc-eew.pl`)だったが、**Go実装が現在の正典であり、Perl版・PerlのCGIビューア(`HTML/`)は共に完全に削除済み**（git履歴には残っている）。リポジトリ名・配布イメージ名の「irc」は、当時IRCにも通知していた頃の歴史的経緯（後述）。

## アーキテクチャ

`cmd/`に薄いエントリポイント、`internal/`に実処理を置く構成。

```text
cmd/eew-notifier/   本体デーモン(WNI受信 → Slack/IRC通知)
cmd/eew-view/       EEWアーカイブの閲覧用Webビューア(元HTML/index.pl+eew-show.pl)
cmd/eew-replay/     開発/検証用: 保存済みテレグラムをdecode→format→(任意で)Slack送信
cmd/wnisim/         開発/検証用: WNI FastCasterプロトコルを模擬するフェイクサーバ

internal/wni/       WNI FastCaster疑似HTTPプロトコルのクライアント実装
internal/wnisim/    wnisimが使うフェイクサーバの実体（wni側のテストからも共用）
internal/decoder/   EEW電文デコーダ（tables_gen.goはPerl版Decoder.pmから生成した対照表）
internal/eewmsg/    デコード済みテレグラム → Slack向けメッセージ整形
internal/eewlog/    生テレグラムのアーカイブ保存（ディレクトリ/ファイル名レイアウトはeew-viewと共有契約）
internal/slack/     Slack incoming-webhook送信（リトライ・レート制限対応）
internal/notify/    Slack hookの「all/limited」振り分け判定
internal/irc/       IRC通知（github.com/fluffle/goirc/clientの薄いアダプタ、詳細は後述）
internal/config/    config.yaml読み込み
internal/app/       上記を束ねてデーモンのメインループを構成
internal/eewview/   Webビューア本体（一覧/詳細/生データダンプ）
```

## ビルド・テスト

```sh
go build ./...
go vet ./...
go test ./...
```

- `internal/decoder`の`TestDecode_RealCorpus`と`internal/eewview`の`TestRenderRealCorpus_NoPanics`は、gitignore対象の実データコーパス`eewlog/`（45,802件の実テレグラム、ローカルにのみ存在）を使う。無ければ`t.Skipf`で自動スキップするのでCIはこれを気にしなくてよい。ローカルで走らせる場合、初回は数十秒〜数分かかることがある（アンチウイルス等のI/O起因、コード起因ではない）。
- `internal/irc`には**実IRCサーバ(testcontainers-goでghcr.io/ergochat/ergoを起動)を使う結合テスト**があり、`//go:build irctest`で`go test ./...`のデフォルトから除外されている。実行するには:

  ```sh
  go test -tags irctest ./internal/irc/...
  ```

  Dockerが必要（GitHub ActionsのCIジョブ`irctest`ではubuntu-latest標準搭載のDockerをそのまま使っている、追加セットアップ不要）。
- Windowsでの`go test -race`はこの環境ではツールチェイン起因で失敗することがある（`0xc0000139`終了コード）。コード側の問題ではなくローカル環境の制約として認識しておくこと。

## 設定ファイル (config.yaml)

`config.yaml-dist`が正典のドキュメント。主なセクション:

- `logdir:` — 生テレグラムの保存先。省略するとアーカイブ無効。
- `irc:` — **YAMLの配列**（複数IRCサーバに対応、Perl版は単一サーバのみだった）。各要素は`server.{host,port,password,charset}` / `nick` / `name` / `desc` / `all-notice` / `limited-notice`（いずれもYAMLリスト）。`charset`未設定時は**UTF-8**がデフォルト（Perl版は`iso-2022-jp`が暗黙デフォルトだったが、意図的に変更済み・ユーザー承認済み）。
- `slack:` — `all`/`limited`の2階層。`all`は常に全報通知、`limited`は新規地震/最終報/取消のみ。
- `WNIEEW:` — WNI認証情報。

**古いPerl形式の`irc:`（単一マッピング）は今は意図的にエラーになる**（`internal/config/config_test.go`の`TestLoad_RejectsOldStyleIRCSection`が仕様として明記）。IRC対応が存在しなかった頃のGo版は`irc:`セクションを無視していたが、今は実際にパースする。

## internal/irc の設計要点

- IRCプロトコル自体は自前実装せず`github.com/fluffle/goirc/client`（本番依存）に委譲。PING/PONGはgoirc内蔵、フレーミングもgoirc任せ。自前で持つのは: config変換、再接続バックオフ、JOIN、文字コード変換、送信キュー、チャンネルディスパッチ。
- 各IRCサーバ接続(`Connection`)は**完全に独立した状態**を持つ: 自分専用の`Dispatcher`（limited-tier判定用`lastEqID`）と自分専用の送信`Queue`。Slackの`notify.Dispatcher`とも他のIRC接続とも状態を共有しない。これはPerl原典（`$last_eq_id`をIRC/Slackで共有していた）からの**意図的な逸脱**。
- 送信`Queue`はeq_id単位でcoalescing（同じ地震の未送信の古い報は新しい報に置き換わる）し、容量超過時は「取消報 > 最終報 > 途中経過」の優先度で間引く。プレーンな`chan`では実現できないため自前実装（`internal/irc/queue.go`）。
- フラッド対策は意図的に入れていない（`goirc`の`Config.Flood = true`で内蔵レート制限も無効化）。Perl版が「対策なし」だったことを踏襲。

## Docker/CI

- `Dockerfile.golang`（notifier）・`Dockerfile.eew-view.golang`（viewer）はどちらも`FROM scratch`のマルチステージビルド。`CGO_ENABLED=0`、タイムゾーンは`time/tzdata`のblank import + `ENV TZ=Asia/Tokyo`（scratchには`/usr/share/zoneinfo`が無い）。
- `.github/workflows/go-test.yaml` — 通常のbuild/vet/test、および`irctest`ジョブ（`-tags irctest`）。
- `.github/workflows/build.yaml` — `v*`タグpush時にghcr.ioへ両イメージを公開。`.github/actions/build`が共通の合成アクション。
- イメージは`ghcr.io/walkure/irc-eew`と`ghcr.io/walkure/eew-view`の2つ。バージョンは`git tag`（例: `v1.3.0`）で管理。

## 移植・変更に関する規律

このリポジトリで作業する際に特に重要な2点（Perl→Go移植中に実際に問題になった経緯があるため）:

1. **Perl原典の挙動・デフォルト値を移植時に黙って変えない。** 変える場合は必ずその場でユーザーに明示し、確認を取ってから進める。過去に`EEW_DATA_DIR`のデフォルト値を`/eewlog/`から`./eewlog/`へ気づかずに変えてしまい、本番のKubernetesデプロイを壊した実例がある。
2. **移植中にPerl側のバグや旧仕様との差異を見つけたら、黙って直す/黙って踏襲するのではなく、必ず一度報告して判断を仰ぐ。** これまでの意図的な変更点（例: IRCの`lastEqID`をサーバごとに独立させた、charsetデフォルトをUTF-8にした、等）はいずれもこの手順を踏んで確認済み。

## Windows開発環境での注意点

- Git Bash(MSYS)経由で`docker`コマンドを使う際、POSIX風パス（`/tmp/...`など）は自動でWindowsパスに書き換えられ、`docker exec`/`docker cp`等の**コンテナ内**パス引数を巻き込んで壊すことがある。コンテナ内パスを渡す場合は`MSYS_NO_PATHCONV=1`を前置する。
- `docker cp`のホスト側ソースパスは、MSYS的な`/tmp/...`ではなく実在するWindowsパス（例: `F:\Repos\irc-eew\...`）を渡すこと。Git BashのPOSIXパスはDockerクライアント（ネイティブWindowsバイナリ）からは解決できない。
- Goツールチェインのインストール場所が`C:\Go`と`C:\Program Files\Go`の間で動くことがあった（`winget upgrade`起因）。`go`コマンドが見つからない場合はまずこれを疑う。

## 用語・経緯

- リポジトリ名・イメージ名の「irc-eew」は歴史的経緯。一時Go移植でIRC対応を落としていた期間があったが、`internal/irc`として復活済み（本ドキュメント作成時点の最新）。
- ローカルでは`irc-eew-go-shadow`という名前のコンテナが本番WNI/Slack/IRC(テスト用Ergoサーバ)に接続した「shadow」検証用インスタンスとして継続稼働していることがある。実地震での動作確認に使う。
