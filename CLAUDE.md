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
- `irc:` — **YAMLの配列**（複数IRCサーバに対応、Perl版は単一サーバのみだった）。各要素は`server.{host,port,password,charset,message-type}` / `nick` / `name` / `desc` / `all-notice` / `limited-notice`（いずれもYAMLリスト）。`charset`未設定時は**UTF-8**がデフォルト（Perl版は`iso-2022-jp`が暗黙デフォルトだったが、意図的に変更済み・ユーザー承認済み）。`message-type`は`notice`（既定、Perl版の`IRCSock::notice`と同じ）または`privmsg`。
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

## Perl版との既知の差異一覧

「移植・変更に関する規律」に基づき確認済みの差異を、種別ごとに記録する。新しい差異を見つけたら黙って直す/踏襲せず、必ずここに追記する前提で判断を仰ぐこと。

**この一覧に載っている項目はすべて確認・承認済みである。**「今回の調査で発見、容認済み」という注記は、その差異を*このセッションで初めて*発見・確認したことを示しているだけで、それ以外の項目（「意図的改善」「バグ修正」等とだけ書かれているもの）も同様に、過去のセッションで同じ手順（発見→報告→ユーザー確認）を経て承認済みである。注記の有無は承認状態の違いを意味しない。

### 通知本体（internal/app, internal/wni）

- WNI再接続: Perlは無制限リトライでbackoffなし。Goは指数バックオフ（1s〜60s上限、接続が一定時間安定した場合のみリセット）。意図的改善。
- WNIサーバーリスト取得: Perlは起動時に1回だけ取得して使い回す。Goは再接続の都度再取得し、失敗時は直近キャッシュにフォールバックする。意図的改善（サーバ側の変更を長時間稼働中でも拾える）。
- WNIプロトコルのフレーミング（`internal/wni/protocol.go`）: Perl(`EEWSock::parse_body`)は受信バッファ長が`Content-Length`と厳密一致(`==`)しないとコールバックが発火せず、Data本体の直後に次ブロックの先頭バイトが同じ`recv()`で届くと永久に詰まるバグがあった。本番Perlの実アーカイブから実際に電文欠落(`20260811075957.4`)を確認済み。Goは`>=`判定で残りバイトを次フレームとして継続処理する、より堅牢な実装。バグ修正（意図的、既に実施済み）。
- `internal/wni/client.go`の`Client.Run`: `FrameParser.Feed`がエラーと同時に返す「パース済みframes」を握りつぶすバグが本番投入後に見つかった（`20260822113559.2`欠落）。修正済み（コミット`279e704`）。Perlとの差異ではなく、Go移植で新たに生まれたバグの修正。

### IRC通知（internal/irc）

（`internal/irc の設計要点`も参照: lastEqIDのサーバ毎独立、charsetデフォルトUTF-8、`irc:`のYAMLリスト化、フラッド対策なしを踏襲、等）

- `message-type`: Perlは常にNOTICE固定。Goは`notice`(既定、Perlと同じ)/`privmsg`を選択可能にする新機能を追加（ユーザー要望）。
- IRC再接続backoff: 接続が一定時間（30秒）安定して初めてリセットするよう修正済み。安定前に切られ続けると即時リトライを繰り返す設計上の穴が本番相当環境で実際に発生したため。

### Slack通知（internal/slack, internal/notify）

- リトライ: Perlは完全に投げっぱなし（1回POSTするだけで、失敗時は何もしない）。Goは429/5xxで最大3回リトライ、`Retry-After`尊重、30秒のタイムアウト予算を持つ。**この差異は無確認のまま実装されていたことが今回の調査で判明し、容認済み**（堅牢性としては改善だが、失敗フックへ同一電文が最大3回POSTされうる・1フックが最大30秒goroutineを占有しうるという副作用がある）。
- それ以外（メッセージ本文/タイトルの組み立て、all/limited振り分け条件、hookの表示名フォールバック等）はPerl(`SlackWebhookSock.pm`/`irc-eew.pl`)と忠実に一致することを確認済み。

### eew-view（internal/eewview）

- `EEW_VIEWER`設定: Perlはhref生成とlighttpdルート定義の2箇所で独立設定（ドリフトしうる）。Goは`ViewerPath`1箇所に統合。意図的改善。
- 一覧ページの「最終報」判定: Perl・Goとも`NCN_type>=8`。通知本体側`Telegram.IsFinal()`（`>0`判定）とは別概念で、意図的に統一していない。
- 詳細ページ震源深度行: Perlは`center_accurate`を誤表示するバグ。Goは`DepthAccurate`を正しく表示。バグ修正（意図的、既に実施済み）。
- 検知日時の日付: Perlは`$wd[2]`（発表日）を誤用するバグ。Goは正しい発生日相当の値を使用。バグ修正（`internal/eewmsg/format.go`の同種修正と同じ考え方、既に実施済み）。
- 詳細ページの`name`パラメータ正規表現: Perlは部分一致。Goは完全一致(`^...$`)に厳格化。意図的な入力検証強化。
- 一覧ページの日別リスト（`?year=&month=&day=`）: Perlは常に年・年月両方へのリンクを表示していたが、Go移植時に脱落していた（本物のリグレッション）。ユーザー報告により発見、修正済み（コミット`6813b39`）。
- Googleマップの表示: Perlは取消報や震央データが無い電文でも無条件に地図を表示していた（結果としてN0.0/E0.0を指す無意味な地図になる）。Goは震央データが無い場合に地図セクション自体を非表示にする。**今回の調査で発見、容認済み**。
- 詳細ページタイトルの「(最終)」表記位置: Perlの`eew_summary`（詳細ページのタイトル生成）は「第3(最終)報」（報の前）、Goは「第3報(最終)」（報の後、一覧ページ側の表記に統一）。Perl自身、一覧側と詳細側で表記が内部的に不統一だった。**今回の調査で発見、容認済み**。
- `name`パラメータ不正時の応答: Perlは常にHTTP 200、本文で「invalid query」（未指定）と「invalid name」（形式不正）を区別していた。Goは常にHTTP 400・「invalid name」に統一。**今回の調査で発見、容認済み**（実害は小さいと判断）。
- 詳細ページのフィールド表示順: Perlは`sort keys %$d`によるほぼ偶然のアルファベット順、Goは意図的に選んだ固定順。**今回の調査で発見、容認済み**（Perl側も設計された順序ではないため実質的な問題ではないと判断）。
- 詳細ページの地図リンクのスキーム: Perlは`http://`、Goは`https://`。**今回の調査で発見、容認済み**（実害なし）。

### 設定ファイル

- `WNIEEW.Logs.Timeout`: Perl版では`send_ack()`が呼ばれるたび（サーバーからの定期pingにackを返す度）に診断ログを1行出すだけのデバッグ用トグルで、実質的な意味は無かった。Go移植時にパースはされたがどこにも配線されておらず、機能していなかった。**今回の調査で発見し、フィールドごと削除**（`Logs.KeepAlive`は引き続き有効）。

## Windows開発環境での注意点

- Git Bash(MSYS)経由で`docker`コマンドを使う際、POSIX風パス（`/tmp/...`など）は自動でWindowsパスに書き換えられ、`docker exec`/`docker cp`等の**コンテナ内**パス引数を巻き込んで壊すことがある。コンテナ内パスを渡す場合は`MSYS_NO_PATHCONV=1`を前置する。
- `docker cp`のホスト側ソースパスは、MSYS的な`/tmp/...`ではなく実在するWindowsパス（例: `F:\Repos\irc-eew\...`）を渡すこと。Git BashのPOSIXパスはDockerクライアント（ネイティブWindowsバイナリ）からは解決できない。
- Goツールチェインのインストール場所が`C:\Go`と`C:\Program Files\Go`の間で動くことがあった（`winget upgrade`起因）。`go`コマンドが見つからない場合はまずこれを疑う。

## 用語・経緯

- リポジトリ名・イメージ名の「irc-eew」は歴史的経緯。一時Go移植でIRC対応を落としていた期間があったが、`internal/irc`として復活済み（本ドキュメント作成時点の最新）。
- ローカルでは`irc-eew-go-shadow`という名前のコンテナが本番WNI/Slack/IRC(テスト用Ergoサーバ)に接続した「shadow」検証用インスタンスとして継続稼働していることがある。実地震での動作確認に使う。
