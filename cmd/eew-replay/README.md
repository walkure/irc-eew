# eew-replay

生のEEWテレグラムファイルを、本番の`eew-notifier`と全く同じ「デコード→メッセージ整形→（任意で）Slack送信」の経路に流し込んで確認するための開発用ツールです。WNIへの実接続は一切行いません。

過去にアーカイブされたテレグラム（`logdir`配下、または`eewlog/`の実データ）を使って、実際に地震が来なくてもメッセージの見た目やSlack通知の動作を手元で確認できます。

## 使い方

```
go run ./cmd/eew-replay <telegramファイル> [<telegramファイル> ...]
```

指定した各ファイルについて、デコード結果から組み立てたタイトルと本文を標準出力に表示します。

```
=== eewlog/2011/03/12/20110312042346.81 ===
title: 高度利用者向け緊急地震速報
text:  2011/03/12 04:26:14 第81報(最終) (2011/03/12 04:23:48発生)震央:<http://maps.google.com/maps?q=38.5,140.8|N38.5/E140.8>(宮城県北部)深さ10km 最大:M6.7 震度6強
```

## Slackへ実送信する

`-slack-webhook`に実際のIncoming Webhook URLを渡すと、整形したメッセージを本当にそのWebhookへPOSTします（各ファイルにつき1回）。テスト用のSlackチャンネル・Webhookを使ってください。

```
go run ./cmd/eew-replay -slack-webhook https://hooks.slack.com/services/xxx/yyy/zzz eewlog/2011/03/12/20110312042346.81
```

送信結果（成功/失敗）はログに出力されます。

## オプション

| フラグ | 既定値 | 説明 |
|---|---|---|
| `-slack-webhook` | (空、送信しない) | 指定するとこのURLへ実際にPOSTする |

## 補足

- 複数ファイルを一度に渡すと、順番にすべて処理されます（同一地震の複数報をまとめて確認する用途を想定）。
- `internal/decoder`・`internal/eewmsg`・`internal/slack`をそのまま呼び出しているので、本番の`eew-notifier`と全く同じロジックで整形・送信されます（このツール自体に独自のフォーマットロジックはありません）。
