# CLAUDE.md

このファイルは、このリポジトリで作業する際にClaude Codeが常に読み込む前提知識です。

## このプロジェクトは何か

緊急地震速報(EEW)を受信し、Slack/IRCへ通知するボット。受信元は元々Weathernews(WNI)の有料配信「FastCaster」だったが、WNIの配信終了に伴い**DMDATA.JPのWebSocket v2 APIへ移行済み**（`v2.0.0`、2026-11-14本番切替、後述「DMDATA移行」）。元はPerl実装(`irc-eew.pl`)だったが、**Go実装が現在の正典であり、Perl版・PerlのCGIビューア(`HTML/`)は共に完全に削除済み**（git履歴には残っている）。リポジトリ名・配布イメージ名の「irc」は、当時IRCにも通知していた頃の歴史的経緯（後述）。

## アーキテクチャ

`cmd/`に薄いエントリポイント、`internal/`に実処理を置く構成。

```text
cmd/eew-notifier/   本体デーモン(DMDATA受信 → Slack/IRC通知)
cmd/eew-view/       EEWアーカイブの閲覧用Webビューア(元HTML/index.pl+eew-show.pl)
cmd/eew-pack/       アーカイブの1年分を<logdir>/<YYYY>.zipにまとめる（eew-viewイメージに同梱、後述「保存サイズ」）
cmd/eew-replay/     開発/検証用: 保存済み電文（DMDATAの*.json・旧WNI電文の両方）を本番と同じ経路で整形→(任意で)Slack送信

internal/dmdata/    DMDATA.JP API v2クライアント（Socket Start/Close + WebSocket v2受信）
internal/dmdatasim/ テスト用のフェイクDMDATAサーバ（チケット使い捨て・ping/pong・接続枠を再現）
internal/eewzip/    年ごとのアーカイブzipの名前規約とPack（eew-pack/eew-viewが共有）
internal/rawarchive/ DMDATAのdataメッセージを受信したまま保存（旧WNIアーカイブと同じYYYY/MM/DDツリー、ファイル名で区別。レイアウトはeew-viewと共有契約）
internal/eew/       配信元に依存しないEEW報モデル、VXSE45 XMLパーサ（値の正規化込み）、地震ID単位の上限付き記憶（RecentSet/OriginCache）
internal/decoder/   旧コード電文(WNI時代)のEEWデコーダ（tables_gen.goはPerl版Decoder.pmから生成した対照表）
internal/eewmsg/    通知文面の整形（FormatReport: VXSE45→旧WNI電文と同形の文面、Format: 旧コード電文用でeew-replayが使用）
internal/slack/     Slack incoming-webhook送信（リトライ・レート制限対応）
internal/notify/    Slack hookの「all/limited」振り分け判定
internal/irc/       IRC通知（github.com/fluffle/goirc/clientの薄いアダプタ、詳細は後述）
internal/config/    config.yaml読み込み
internal/app/       上記を束ねてデーモンのメインループを構成
internal/eewview/   Webビューア本体（一覧/詳細。store.goでフォルダと年zipを1つの一覧にまとめる）
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
  この開発機（Windows + Docker Desktop）ではtestcontainersの後片付け用コンテナ（ryuk）が起動せず`reaper: … "Started" matched 0 times`で全テストが失敗するので、`TESTCONTAINERS_RYUK_DISABLED=true go test -tags irctest ./internal/irc/...`で実行する（コード側の問題ではない）。失敗時に未起動のergoコンテナ（`Created`状態）が残ることがあるので、その場合は作成日時を確認して削除する。
- Windowsでの`go test -race`はこの環境ではツールチェイン起因で失敗することがある（`0xc0000139`終了コード）。コード側の問題ではなくローカル環境の制約として認識しておくこと。
- この開発機（Windows）では、テストを多数並列に走らせたとき（`go test ./...`やテストバイナリの並列実行）に、ループバック上の**新しいWebSocket接続の最初の書き込みが2〜13秒止まる**ことがあり、`internal/dmdata`の5秒待ちのテスト（例: `TestRunSession_NonClosingServerErrorKeepsSession`）がまれに落ちる。同じテストをLinuxコンテナ（golang:1.25-alpine、CIと同等）で8並列×40回流すと失敗0・100ms超の書き込み0（2026-11-13に計測）。コード起因ではなくこの環境（セキュリティソフトの通信検査等と推測）の制約なので、テストの待ち時間を延ばして誤魔化さないこと（ユーザー方針）。疑わしいときはLinuxコンテナで再現を確認する:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "F:\Repos\irc-eew:/src:ro" -w /src -e GOFLAGS=-buildvcs=false golang:1.25-alpine go test ./...`

## 設定ファイル (config.yaml)

`config.yaml-dist`が正典のドキュメント。主なセクション:

- `logdir:` — 受信電文の保存先。省略するとアーカイブ無効。旧WNIアーカイブ（`<地震ID>.<報番号>`、Shift_JISのコード電文）と同じ`YYYY/MM/DD`（地震IDの日付）のツリーに、DMDATAの受信メッセージを`<EventID>.<Serial>_<受信時刻>_<メッセージID>.json`（受信バイト列そのまま）として保存する（ユーザー決定: 新旧の保存パスを揃える）。EventID/Serialは外側JSONの`xmlReport.head`から取り、取れなければ受信日時のフォルダに`<受信時刻>_<メッセージID>.json`。eew-viewは`EEW_DATA_DIR`1つで新旧両方を読む。
- `irc:` — **YAMLの配列**（複数IRCサーバに対応、Perl版は単一サーバのみだった）。各要素は`server.{host,port,password,charset,message-type}` / `nick` / `name` / `desc` / `all-notice` / `limited-notice`（いずれもYAMLリスト）。`charset`未設定時は**UTF-8**がデフォルト（Perl版は`iso-2022-jp`が暗黙デフォルトだったが、意図的に変更済み・ユーザー承認済み）。`message-type`は`notice`（既定、Perl版の`IRCSock::notice`と同じ）または`privmsg`。
- `slack:` — `all`/`limited`の2階層。`all`は常に全報通知、`limited`は新規地震/最終報/取消のみ。
- `dmdata:` — `api-key`、`app-name`、`api-base`（偽サーバ向け）のみ（保存先はトップレベルの`logdir`）。受信する区分(`eew.forecast`)・種別(`VXSE45`)・テスト電文なし・`formatMode: raw`は通知処理がそれしか扱えないため**設定項目にせずコードに固定**（`internal/app`、ユーザー決定: 変更不要な設定項目は置かない）。

**古いPerl形式の`irc:`（単一マッピング）は今は意図的にエラーになる**（`internal/config/config_test.go`の`TestLoad_RejectsOldStyleIRCSection`が仕様として明記）。IRC対応が存在しなかった頃のGo版は`irc:`セクションを無視していたが、今は実際にパースする。

## internal/irc の設計要点

- IRCプロトコル自体は自前実装せず`github.com/fluffle/goirc/client`（本番依存）に委譲。PING/PONGはgoirc内蔵、フレーミングもgoirc任せ。自前で持つのは: config変換、再接続バックオフ、JOIN、文字コード変換、送信キュー、チャンネルディスパッチ。
- 各IRCサーバ接続(`Connection`)は**完全に独立した状態**を持つ: 自分専用の`Dispatcher`（limited-tier判定用`lastEqID`）と自分専用の送信`Queue`。Slackの`notify.Dispatcher`とも他のIRC接続とも状態を共有しない。これはPerl原典（`$last_eq_id`をIRC/Slackで共有していた）からの**意図的な逸脱**。
- 送信`Queue`はeq_id単位でcoalescing（同じ地震の未送信の古い報は新しい報に置き換わる。ただし送り先チャンネルは新旧の和集合、優先度は高い方を残す。2026-11-13に実地震で、初報（#limited宛て）が65ms後の第2報（#all宛てのみ）に置き換わって#limitedに何も届かない不具合が起きたため修正）し、容量超過時は「取消報 > 最終報 > 途中経過」の優先度で間引く。プレーンな`chan`では実現できないため自前実装（`internal/irc/queue.go`）。
- フラッド対策は意図的に入れていない（`goirc`の`Config.Flood = true`で内蔵レート制限も無効化）。Perl版が「対策なし」だったことを踏襲。

## Docker/CI

- `Dockerfile`（notifier）・`Dockerfile.eew-view`（viewer）はどちらも`FROM scratch`のマルチステージビルド。`CGO_ENABLED=0`、タイムゾーンは`time/tzdata`のblank import + `ENV TZ=Asia/Tokyo`（scratchには`/usr/share/zoneinfo`が無い）。
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
- limited振り分けの「新しい地震」判定（DMDATA移行時に変更、ユーザー承認済み）: Perlは直前1件のeq_idと比較していたため、複数地震の報が交互に届くと切り替わるたびにlimitedが発火した。Goは直近64地震分のEventIDを記憶（`eew.RecentSet`）し、各地震の初報・最終報・取消報のみlimitedへ送る。IRCの`irc.Dispatcher`も同じ方式（接続ごとに独立した記憶）。

### eew-view（internal/eewview）

- `EEW_DATA_DIR`の既定値: Perlは`/eewlog/`。Go移植時（`e70a8e8`）から`./eewlog/`に黙って変わっていたのを2026-11-13に発見し、`/eewlog/`に戻した（`acf8eb9`、ユーザー指示）。4つの環境変数の既定値は`TestLoadConfigFromEnv_PerlDefaults`で固定。
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
- **DMDATA移行での改修（ユーザー決定: 閲覧者に新旧の差を意識させない）**: `EEW_DATA_DIR`の同じ`YYYY/MM/DD`ツリーから旧WNI電文とDMDATA VXSE45（`<EventID>.<Serial>_<受信時刻>_<id>.json`）の両方を読み、どちらも共通の表示用レコード（`record.go`）に変換して同じテンプレートで表示する。一覧は地震ID→報番号→受信順。詳細の`?name=`は旧は`<eq_id>.<n>`のまま、新はファイル名（`.json`除く）。
  - 生データ（旧のSJIS電文ダンプ、新のXML）は表示しない（ユーザー決定。VXSE45のXMLは1報最大20KB程度で画面が肥大化するため）。
  - 不明値は新旧とも「不明」表示（旧は`///`を0として`深さ0km`/`M0.0`/`N0.0/E0.0`と表示していた）。`decoder.Telegram`に`CenterUnknown`等のフラグを追加して判定（ユーザー決定）。
  - VXSE45にしかない値（検知日時=ArrivalTime、内陸/海域、最大長周期地震動階級、予測の変化、付加文、本文、地域表の長周期列）は値がある時だけ然るべき位置に表示（ユーザー決定）。精度ラベルは旧電文は当時の表（RK1〜3: テリトリー法等）、VXSE45は気象庁現行の表（IPF法等）。
  - 旧電文の2行目の時刻のラベルを「検知日時」→「発生日時」に変更（コード電文解説資料で「地震発生時刻もしくは地震検知時刻」、実データでも発生時刻相当。VXSE45では発生日時=OriginTime、検知日時=ArrivalTimeを別々に表示するため）。ユーザー承認済み（原典＝コード電文解説資料に合わせる）。
  - 旧電文の地域表を修正: 予測震度の幅はe1e2が上限・e3e4が下限なのにPerl由来の表示は「上限～下限」の順だったので「下限～上限」に。到達時刻欄は`//////`（不明・未設定）を「既に到達」と表示していたのを、y2=1を「既に到達」、`//////`を「不明」に。地域表に「区分」（警報/予報、旧はy1）列を追加。いずれもユーザー承認済み。

### 設定ファイル

- `WNIEEW.Logs.Timeout`: Perl版では`send_ack()`が呼ばれるたび（サーバーからの定期pingにackを返す度）に診断ログを1行出すだけのデバッグ用トグルで、実質的な意味は無かった。Go移植時にパースはされたがどこにも配線されておらず、機能していなかった。**今回の調査で発見し、フィールドごと削除**（`Logs.KeepAlive`は引き続き有効）。

## DMDATA移行（2026-11-14本番切替済み）

WNIのFastCaster配信が終了したため、WNI関連コード（`internal/wni`・`internal/wnisim`・`cmd/wnisim`・`WNIEEW:`設定）は削除済み（git履歴には残っている）。以下の「Perl版との既知の差異一覧」のうちWNIプロトコル関連の項目は経緯として残している。

- 受信: VXSE45（緊急地震速報（地震動予報）、区分`eew.forecast`）のみ。VXSE44は二重通知回避のため使わない。
- DMDATAはEEWを気象庁XML（`formatMode: raw`）かDMDATA独自JSON（`formatMode: json`）で配信し、旧コード電文は配信されない。`format-mode`は`raw`（気象庁XML原本）で受信する（ユーザー決定、既定値として明示固定）。
- **VXSE45を`raw`で受けた本文は気象庁防災情報XMLで確定**（2026-11-12、ユーザー確認済み）。根拠: DMDATA電文一覧でVXSE45の配信形式はXMLのみ、気象庁解説資料でもXML電文として定義、実受信（2地震・計10報）がすべて`format: xml`/gzip+base64。Socket Startの`formats`に`a/n`・`binary`が含まれるのは区分としての受信可能形式であり、VXSE45がそれらで来ることを意味しない。実装は「VXSE45 XMLのみ解析」とし、`format`がxml以外・`head.type`がVXSE45以外の電文は通知せず警告ログのみ（生データ保存は従来どおり）。
- **生データ保存はバイト列として扱う**（ユーザー決定）: WebSocketで受信した`data`メッセージのバイト列を、デコード・再エンコード・JSON再整形・正規化を一切せずにそのまま書く（`rawarchive.Save`に渡すのは`DataMessage.Raw`＝受信バイト列そのもの）。中身の解釈・正規化は解析層だけで行い、保存データには決して反映しない。
- **XMLテキストの正規化方針**（ユーザー決定、フェーズ2で実装）: 気象庁XMLの要素の中身・属性値を判定に使う前には必ず正規化する。2026-10-16の千島列島/カムチャツカの地震でdocomoのエリアメールが「従来と一部形式が異なるデータ」で配信できず、`ReduceName`の値に改行が混入していた疑いがSNSで指摘された（未確認）ため。
  - 判定用の値（`InfoType`、`NextAdvisory`の文言、震度`From`/`To`、M、座標、各種コード、`rank2`/`condition`/`type`等の属性）: 空白類（半角空白・タブ・CR・LF・全角空白）を前後だけでなく途中も含めてすべて除去してから比較・パースする。最終報判定は正規化後の`NextAdvisory`が「この情報をもって、緊急地震速報：最終報とします。」と完全一致すること（要素はあるが不一致なら最終報扱いせず警告ログ）。
  - 表示用の値（震央名、取消報の`Text`等）: 前後の空白類を除去し、途中のCR/LFは半角空白1つに置換する（途中の空白を詰めない。解説資料の`Text`事例のように改行が区切りの意味を持つため）。
  - 正規化は空白類の除去のみ。NFKC等の文字種変換はしない（仕様外の値をもっともらしい値に化けさせないため）。正規化後も仕様の値域に入らない値は「不明」として扱い、警告ログを出すが通知自体は止めない。
  - 防御の二段目として、IRC/Slackへの送信直前に完成文面全体からCR/LFを除去する。IRCは1メッセージ＝CR-LF終端の1行で、本文(trailing)にNUL/CR/LFを含められない（RFC 1459/2812 §2.3、RFC 1459 §8は「CRかLFが単独で現れても行末とみなす実装がある」と注記）。goirc v1.3.5の`Conn.Raw`はコマンド注入防止のため`cutNewLines`で**最初のCR/LF以降を黙って切り捨てる**ので、改行が混入すると注入にはならないが、震央名以降（深さ・M・震度）が通知から無言で欠落する。
- VXSE45の一次資料は気象庁「地震火山関連 解説資料」（xml.kishou.go.jp の技術資料ページの解説資料セット内、緊急地震速報の内容部はⅡ.21節）。最終報=`Body/NextAdvisory`の出現（`Epicenter@rank2="9"`は「震源・Mの精度が最終報相当」で最終報の目印ではない、公式サンプルでも最終報前から9になる）、取消=`Head/InfoType`が「取消」（本文は`Text`のみ・`Serial`は直前と同じ番号のまま＝EventID+Serialは一意でない・最終報を経ずに取消で終わることがある）。
- Slack/IRCへ流す文面は既存フォーマット（`internal/eewmsg`）を踏襲する（ユーザー要望）。XMLから再現できない差異が出る場合は個別に報告して判断を仰ぐ。
- 段階: (1)受信+生データ保存（2026-11-12完了、実受信で確認） / (2)VXSE45解析+通知（完了、実地震2件で確認） / (3)eew-view対応（完了。保存形式の縮小は後述「保存サイズ」） / (4)docs・Docker・本番切替（2026-11-14に`v2.0.0`で本番切替済み。本番は`kamo-river/agano-k8s-applications`の`namespaces/slackbot/shared`、`app-name: irc-eew`。WebSocketは1接続しか張れない契約なので、shadowを動かすときは本番と同時に動かせない＝shadowのnotifierは止めてある）。
- **通知文面（ユーザー決定）**: 旧WNIコード電文由来の形式を踏襲する。`{発表時刻} 第{Serial}報[(最終)][(訂正)] ({発生時刻}発生)震央:<地図URL|N{緯度}/E{経度}>({震央名})深さ{D} 最大:M{M} 震度{I}`、取消は`{発表時刻} 第{Serial}報 ({発生時刻}発生) 取り消されました`、タイトルは「高度利用者向け緊急地震速報」のまま。対応と決定事項:
  - 発表時刻=`ReportDateTime`、報番号=`Serial`（取消報もXMLの値そのまま。旧電文と違い取消で番号は増えない）、発生時刻=`OriginTime`（旧電文2行目「地震発生時刻もしくは地震検知時刻」と同義、コード電文解説資料で確認）、震央名=`Hypocenter/Area/Name`（`ReduceName`ではない）。
  - 発生時刻が無い報（最大予測震度のみ等）は「(…発生)」部分ごと出さない。取消報は同じEventIDの直前報の`OriginTime`を使う（`eew.OriginCache`、地震IDごと・直近64地震分。複数地震の速報が同時に来るため）。覚えていなければ出さない。
  - 不明値は「不明」と表示（旧実装は0扱いで`深さ0km`/`M0.0`/`N0.0/E0.0`だったが、深さ0kmは核実験等と区別できないため変更）: `深さ不明`、`M不明`、座標不明は地図リンクなしの`震央:不明(…)`。`ごく浅い`/`700km以上`はそのまま表記。
  - 仮定震源要素（PLUM法のみ、`Condition`=仮定震源要素）の深さ10km・M1.0は固定の仮の値なので`深さ仮定 最大:M仮定`と表示し、位置（トリガー観測点の座標）と地名はそのまま出す（ユーザー決定、案1）。eew-viewも同じ（旧電文はRT n3=9で判別、`decoder.Telegram.AssumedHypocenter`）。
  - 予測震度は`ForecastInt`のFrom/Toで表現: 同値`震度6弱`、To=over`震度5弱程度以上`、幅あり`震度4から5強程度`、不明または要素なし`震度不明`、0は`震度0`（旧コード電文は0と不明が同じ`//`だった）。
  - 訓練/試験（`Control/Status`が通常以外）・VXSE45以外・XML以外・解析不能な電文は通知しない（警告ログのみ、生データは保存済み）。`InfoType`訂正は報番号の後ろに`(訂正)`を付けて通知する（最終報の訂正は`第N報(最終)(訂正)`）（ユーザー決定）。
- 起動時と接続数上限(409)時に、同じ`app-name`で残っている`open`/`waiting`ソケットをSocket List→Socket Closeで掃除する（前回プロセスが異常終了した場合の枠解放）。**同じAPIキーでshadowと本番を同時に動かすときは`app-name`を必ず分けること**（同名だと互いの接続を切る）。APIキーに必要な権限: `socket.start`・`socket.list`・`socket.close`・`eew.get.forecast`。
- エラー処理（`internal/dmdata/errors.go`の`Classify`）: リトライで直らないもの（Socket Startの400/401/402/403、WSの4400/4807、要求した区分・種別が許可されない）は**プロセスを終了コード1で終了**（「未課金ならエラーで終了すべき」というユーザー要望に基づく。契約切れ等を再起動ループとして可視化する）。それ以外（409/429/5xx/通信エラー、WSの4404/4409/4640/4641/4891/未知コード、および4808＝コントロールパネル等からの明示的切断〔ユーザー判断で一時的扱い〕）はバックオフで再接続。4503（サーバー再起動）はログレベルがINFOになるだけで、待ち時間は「30秒以上安定していたら1秒に戻す」通常規則に任せる（接続直後に4503を返し続ける障害で1秒間隔の再接続を繰り返さないため）。
- 生データは`data`メッセージ全体を受信したまま保存する方針（ユーザー承認済み）。閲覧用の派生形式（CBOR等）が要れば段階3で検討。
- **保存サイズ（年ごとのzip、2026-11-14に本番適用済み）**:
  - 実測: 本番の旧アーカイブ（agano `/var/lib/eewlog/data`、local PV、PVCは2Gi、ブロック4KB）は約66,000件・中身13MBだがディスク上は275MB（1件4KBブロックを占有）。DMDATAの保存ファイルは1件2.2〜2.4KB（大地震でも約4KB）なので、ディスク上はどちらも1件4KB。
  - 方式（ユーザー決定）: 締まった年を`<logdir>/<YYYY>.zip`（エントリ名`YYYY/MM/DD/<元のファイル名>`、中身は元ファイルのバイト列そのまま＝「受信バイト列のまま保存」を維持）にまとめる。tar.gz等はファイル単位のランダムアクセスができないため不採用。DMDATAも同じ運用（年が明けたら前年をまとめる）。旧電文はもう増えないので、2026年は`-legacy-only`で旧電文だけ先にまとめられる。
  - `eew-pack`（`internal/eewzip.Pack`）: 既存zipがあればマージ（同名でバイト列が違うとエラーで既存zipを変えない）、一時ファイルに書いて全件をバイト単位で照合してからrenameで置き換え、権限は0644、元ファイルは消さない。`-move-packed DIR`を付けると、zipに入れて照合済みの元ファイルを`DIR/YYYY/MM/DD/<file>`へ移し（上書きはしない、移動先にあればエラー）、空になった年のディレクトリを消す（ユーザー決定: 削除ではなく移動）。DIRはeew-viewが読む場所（logdir自体・数字名の年ディレクトリ）だと拒否する。`YYYY/MM/DD/<file>`の外のファイルは入れずに一覧表示する。
  - eew-view: フォルダとzipを1つの一覧にまとめる（同じ名前が両方にあればフォルダ側を読む）。年のzipの目録は初めてその年を見たときに読み込み、zipのサイズ/更新時刻が変われば読み直す。zipは開きっぱなしにせず、目録のオフセットから直接読んでCRC照合する（Windowsでは開いているファイルを置き換えられないため、また作り直し中も表示できるように）。
  - 実測（2026-11-14、本番の旧アーカイブ全年をaganoでまとめて手元で計測）: 16年分65,900件→zip合計20MB（ディスク上275MB）。全一覧4,340ページ＋詳細8,318件を巡回したときのeew-viewのprivateメモリは、フォルダのまま54MB、zipのみ67MB（全年の目録がGoヒープで6.7MB＝1件約100B、残りはGCの余裕分）。応答速度は同等。
  - 本番アーカイブの非正規ファイル: `2022/12/temp/{06,07,09,10}/`の49件（正規の場所にも同名があり、うち4件は中身が異なる。2022年12月の作業跡と思われる）はzipに入れず、下記のバックアップにのみ残っている。直下の空ファイル`test`はユーザーが削除済み。報番号のない`2011/03/13/20110313082415`・`2011/03/14/20110314134555`はzipには入るがeew-viewの一覧には出ない（従来どおり）。
  - 本番適用（2026-11-14、v2.1.0）: aganoで2010〜2025年と2026年の旧電文（`-legacy-only`）をzip化し、元ファイル65,949件はホストの`/var/lib/eewlog/loose/`（PVの外、Podからは見えない）へ手作業で移動したのち、`xz -9e`のtarで**agano の`~walkure/eewlog-loose-20261114.tar.xz`**（1.36MB、65,949件、sha256 `c45bd8b6a94696457f4b73785a3652b40d085ed200e3e997181a1ff37812b975`、`tar -d`で全件一致を確認）にバックアップし、`loose/`は削除済み（ユーザー実施）。本番eew-viewの全70,245ページが手元で確認済みのzipのみの表示とハッシュ一致。shadowのみが受信した9件（2026-11-12/13）も本番へ転送済み。段階1の検証中に受信した実電文10件（2026-11-12の`20261112113529`第1〜4報・`20261112130734`第1〜6報、当時は`<受信時刻>_<id>.json`名）も、中身はそのまま名前だけ現行の`<EventID>.<Serial>_<受信時刻>_<id>.json`に揃えて本番へ転送済み。手元（shadow・Dockerのvolume・`eewlog/`コーパス・作業用ディレクトリ）に本番にない受信データが残っていないことを照合済み。
  - **年次作業はJobで行う（ユーザー決定: CronJobではなく普通のJob）**: agano-k8s-applicationsの`namespaces/slackbot/shared/job-eew-pack.yaml`に`eew-pack-<年>`という名前のJob（eew-viewイメージの`eew-pack -dir /eewlog -year <年> -move-packed /eewlog/packed`、uid 1000、PVをrwでマウント）を置いてある。**毎年1月に、Jobの名前と`-year`を前年に書き換えてpushする**と、ArgoCDが前のJobを消して新しいJobを1回実行する（完了済みJobのspecは変更できないので名前を変えるのが必須）。2026年分は2027年1月に実行する（2026年の旧電文は既にzip内、DMDATA分が追加される）。何度実行しても結果は同じ。Jobのイメージは同じディレクトリのkustomizationの`eew-pack`という別名で固定している（eew-viewのタグを上げると完了済みJobのspecが変わってArgoCDの同期が失敗するため）。名前を変えるときに一緒に上げる。2026年分は**v2.2.1以上**で実行すること（v2.2.0の`eew-pack`はtzdata未同梱でscratchイメージ内ではzipエントリのローカル時刻がUTCになる。中身のバイト列には影響なし）。2026年分の動作は、本番と同じ状態（旧電文のみの2026.zip＋DMDATA13件）を手元とLinuxイメージで再現して確認済み（zipは両者でsha256一致、eew-viewの2026年の全ページが実行前後で同一）。
  - 保存ファイルの権限: `rawarchive.Save`が`os.CreateTemp`の0600のまま置いていたため、notifierと別ユーザー（aganoのssh利用者等）から読めなかった。0644に修正（ユーザー決定。umask任せではなく固定）。v2.0.0で保存された分は本番で手作業で`chmod 644`済み。

## Windows開発環境での注意点

- Git Bash(MSYS)経由で`docker`コマンドを使う際、POSIX風パス（`/tmp/...`など）は自動でWindowsパスに書き換えられ、`docker exec`/`docker cp`等の**コンテナ内**パス引数を巻き込んで壊すことがある。コンテナ内パスを渡す場合は`MSYS_NO_PATHCONV=1`を前置する。
- `docker cp`のホスト側ソースパスは、MSYS的な`/tmp/...`ではなく実在するWindowsパス（例: `F:\Repos\irc-eew\...`）を渡すこと。Git BashのPOSIXパスはDockerクライアント（ネイティブWindowsバイナリ）からは解決できない。
- Goツールチェインのインストール場所が`C:\Go`と`C:\Program Files\Go`の間で動くことがあった（`winget upgrade`起因）。`go`コマンドが見つからない場合はまずこれを疑う。

## 用語・経緯

- リポジトリ名・イメージ名の「irc-eew」は歴史的経緯。一時Go移植でIRC対応を落としていた期間があったが、`internal/irc`として復活済み（本ドキュメント作成時点の最新）。
- 実地震での動作確認（shadow）は`dev/shadow/`のdocker composeで行う（2026-11-14の本番切替後、ユーザー指示で`docker compose down`済み。`dev/shadow/data/`と設定は残してある。再開するときは本番とWebSocket接続が競合しないよう注意）（ユーザー要望: IRCも含めて試す）。作業ツリーからビルドしたnotifier（`irc-eew-shadow-notifier-1`）が本物のDMDATAに接続し、同じcompose内のErgo IRCサーバ（`irc-eew-shadow-ergo-1`、`#eew`/`#eew-limited`）とconfig.yamlに書いたSlack（テスト用チャンネルのみ）へ通知する。Ergoは`127.0.0.1:6667`に公開しているので、この開発機のIRCクライアントから接続して通知を目視できる（他ホストからは不可）。`dev/shadow/config.yaml`（秘密情報）と`dev/shadow/data/`（受信生データ）はgitignore。DMDATAの`app-name`は`irc-eew-shadow`で、他インスタンスと分けている。Ergoは履歴を持たないので、常時joinしている監視クライアント`irc-eew-shadow-observer-1`（`dev/shadow/irc_observer.py`、goirc/internal/ircに依存しない独立実装）がチャンネルに流れたNOTICE/PRIVMSGを全部ログに出す。IRCに実際に何が送られたかは`docker compose logs observer`で確認する（Slackの着信はClaudeからは見えないため、通知内容の確認はこちらで行う）。更新は`docker compose up -d --build`（`dev/shadow`で実行）。
