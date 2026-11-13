# eew-replay

保存済みの電文ファイルを、本番の`eew-notifier`と同じ「解析→メッセージ整形→（任意で）Slack送信」の経路に流し込んで確認するための開発用ツールです。DMDATA.JPなどへの実接続は一切行いません。

`logdir`配下に保存された電文を使って、実際に地震が来なくてもメッセージの見た目やSlack通知の動作を手元で確認できます。新旧どちらの形式も受け付けます。

| ファイル | 形式 | 処理 |
|---|---|---|
| `*.json` | DMDATAから受信した`data`メッセージ（`<EventID>.<Serial>_<受信時刻>_<id>.json`） | 本番と同じ`app.Processor`（VXSE45の解析→`eewmsg.FormatReport`） |
| それ以外 | 旧WNIのコード電文（`<地震ID>.<報番号>`） | `internal/decoder`→`eewmsg.Format` |

## 使い方

```
go run ./cmd/eew-replay <電文ファイル> [<電文ファイル> ...]
```

指定した各ファイルについて、組み立てたタイトルと本文を標準出力に表示します。

```
=== logdir/2026/11/13/20261113105223.5_20261113105303.217_242eb2f2….json ===
title: 高度利用者向け緊急地震速報
text:  2026/11/13 10:53:02 第5報(最終) (2026/11/13 10:52:20発生)震央:<http://maps.google.com/maps?q=32.5,130.6|N32.5/E130.6>(熊本県熊本地方)深さ10km 最大:M3.9 震度3
```

## Slackへ実送信する

`-slack-webhook`に実際のIncoming Webhook URLを渡すと、整形したメッセージを本当にそのWebhookへPOSTします（各ファイルにつき1回）。テスト用のSlackチャンネル・Webhookを使ってください。

```
go run ./cmd/eew-replay -slack-webhook https://hooks.slack.com/services/xxx/yyy/zzz logdir/2026/11/13/*.json
```

送信結果（成功/失敗）はログに出力されます。

## オプション

| フラグ | 既定値 | 説明 |
|---|---|---|
| `-slack-webhook` | (空、送信しない) | 指定するとこのURLへ実際にPOSTする |

## 補足

- 複数ファイルを一度に渡すと、渡した順にすべて処理されます。DMDATAの取消報は本番と同じく、それより前に渡した同じ地震IDの報の発生時刻を表示に使うので、同じ地震の報は受信順に渡してください（ファイル名順のglobで受信順になります）。
- 本番が通知しない電文（VXSE45以外、訓練・試験、解析できないもの）は、理由をログに出して表示しません。
- 整形はすべて本番と同じコード（`internal/app`・`internal/eewmsg`・`internal/decoder`・`internal/slack`）を呼び出しており、このツール自体に独自のフォーマットロジックはありません。
