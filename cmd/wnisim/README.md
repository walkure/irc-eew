# wnisim

WNI FastCaster（`eew-notifier`が受信しているWNIのEEW配信プロトコル）を模擬するフェイクサーバです。実際のWNIサーバに接続できない環境でも、プロトコルの疎通・再接続・タイムアウトまわりの動作を手元で検証するための開発・検証用ツールです。本番の一部ではありません。

ログイン応答、`Content-Length`付きDataブロックの送信、Keep-Alive、そしてWNI独特のサーバー発信`GET / HTTP/1.1`ACKクイーク（[README.ja.md](../../README.ja.md)の「ちぅい」参照）を再現します。この実装自体は、実績10年のPerl版（`irc-eew.pl`/`EEWSock.pm`）を実際にこのフェイクサーバへ接続して動作確認することで、プロトコルの忠実性を検証済みです。

## 使い方

事前に、配信したい生テレグラムファイルをディレクトリにまとめておきます（`eewlog/`の実データや、`internal/decoder/testdata/`のフィクスチャなど）。

```
go run ./cmd/wnisim -telegrams-dir ./sample-telegrams -listen :19000
```

クライアント（`eew-notifier`や自作のテストコード）を、`server-override: 127.0.0.1:19000`のようにこのアドレスへ向けて接続させます（`eew-notifier`の`config.yaml`の`WNIEEW.server-override`は検証用途専用のオプションです）。

接続を受け付けると、ログイン応答 → 指定ディレクトリ内のファイルをファイル名順に1つずつData ブロックとして送信 → 全件送信後はアイドル状態を維持、という流れで動きます。

## オプション

| フラグ | 既定値 | 説明 |
|---|---|---|
| `-listen` | `:19000` | 待受アドレス |
| `-telegrams-dir` | (必須) | 配信するテレグラムファイルの入ったディレクトリ |
| `-interval` | `3s` | テレグラム送信の間隔 |
| `-keepalive-interval` | `30s` | Keep-Alive送信間隔（`0`で無効化） |
| `-get-ping-every` | `1` | 何件ごとに`GET / HTTP/1.1`ACKクイークを送るか（`0`で無効化、`1`なら毎回） |
| `-once` | `false` | 1接続を処理したら終了する（既定は接続が切れても待ち受けを継続） |

## 検証例

```
# フェイクサーバを起動
go run ./cmd/wnisim -telegrams-dir ./eewlog/2011/03/12 -interval 2s -once

# 別ターミナルで、server-overrideを使ったconfig.yamlを用意してeew-notifierを接続
go run ./cmd/eew-notifier /path/to/test-config.yaml
```

ログに`sent GET / HTTP/1.1 ping, waiting for ack...`→`client acked the GET ping`が出ていれば、クライアント側がACKクイークに正しく応答できています。応答がない場合は`WARNING: no ack received for GET ping within timeout`と出ます。

## 補足

- `internal/wnisim`パッケージを薄くラップしたCLIです。`internal/wni`のテストコード（`client_test.go`等）でも同じ`internal/wnisim`が使われており、そちらは`go test`から直接呼ばれるため通常このCLIを意識する必要はありません。このCLIは「人間が目視で確認したい」「Perl版など別のクライアント実装を検証したい」ときに使います。
- 空ファイル（0バイト）はスキップされます。
