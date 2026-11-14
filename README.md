# irc-eew // EEW IRC/Slack bot

## なにこれ

[DMDATA.JP](https://dmdata.jp/)のWebSocket v2 APIから緊急地震速報（地震動予報、気象庁の電文VXSE45）を受信して、IRC/Slackへ流す為の単純な何か。

## 使い方

DMDATA.JPで、VXSE45が配信される「緊急地震（予報）」区分（`eew.forecast`）に課金してください。
APIキーには`socket.start`・`socket.list`・`socket.close`・`eew.get.forecast`の権限が必要です。

config.yaml-distを適当に書き換えて、config.yamlにすれば引数指定しなくても読みに行きます。
`dmdata:`の`api-key`にAPIキーを書いてください。

limited-notice（IRC）・`limited`（Slack）には、地震ごとの第1報・最終報・取消報だけを通知します。
all-notice・`all`は全部投げる。

`irc:`はリストなので、複数のIRCサーバへ同時に通知できます。
各サーバの`charset`は省略するとUTF-8になります。
まだISO-2022-JPを使っているサーバ/チャンネル向けには`charset: iso-2022-jp`を設定してください。
フラッド対策はIRC側にも入れていません。

受信した電文を保存するエントリ「logdir:」行をコメントアウトすると、電文を保存しなくなります。
保存する電文はDMDATA.JPから受け取ったままの形です（WNI時代の電文と同じディレクトリに並べて置きます）。

同じAPIキーで複数のインスタンスを動かす場合は、`dmdata:`の`app-name`をインスタンスごとに変えてください。
起動時に、同じ名前で残っている接続を掃除するためです。

APIキーが無効・契約がない・権限が足りない・契約が解除された、などリトライしても直らないエラーでは、
エラーを出してプロセスが終了します（Kubernetes等で気づけるように）。一時的なエラーは間隔を空けて再接続します。

コンテナで動かす場合は以下のような感じで：

`docker run -it --rm --mount type=bind,source=/usr/local/irc-eew/config,target=/conf,readonly ghcr.io/walkure/irc-eew:latest`

## EEW Viewer

保存した電文（`logdir:`）をブラウザで見るためのビューアもあります。DMDATA.JPから受信したものと、
WNI時代のものを同じ一覧に並べて表示します（読み取り専用）。

`docker run -it --rm -p 8080:8080 --mount type=bind,source=/a/path/to/eewlog,target=/eewlog,readonly ghcr.io/walkure/eew-view:latest`

設定は環境変数で行います。

|名前|既定値|説明|
|----|------|----|
|`EEW_DATA_DIR`|`/eewlog/`|電文の保存先（`logdir`）|
|`EEW_PATH_BASE`|`./`|URIのパスのベース|
|`EEW_VIEWER`|`eew-show`|詳細表示のルート名|
|`EEW_LISTEN_ADDR`|`:8080`|待ち受けアドレス|

### 年ごとのzipにまとめる

電文は1件ずつのファイルなので、ディスクのブロック単位で場所を取ります。年が明けたら前年の分を
`eew-pack`で`<logdir>/<年>.zip`にまとめられます。ビューアはフォルダとzipの両方を読むので、
まとめたあとも表示は変わりません。`eew-pack`は元のファイルを消しません。`-move-packed`を付けると、
zipに入れて照合し終えたファイルを指定した場所（例：`/eewlog/packed`、ビューアからは見えない）へ移します。
`eew-pack`はeew-viewのイメージに入っています。

`docker run -it --rm --mount type=bind,source=/a/path/to/eewlog,target=/eewlog --entrypoint /usr/src/eew-view/eew-pack ghcr.io/walkure/eew-view:latest -dir /eewlog -year 2025 -move-packed /eewlog/packed`

まだ電文が増える年の旧WNI電文だけをまとめるときは`-legacy-only`を付けます。

## ちぅい

DMDATA.JPのWebSocketサーバは月1回程度メンテナンスで再起動するので、そのたびに再接続します。

## 作者

walkure at 3pf.jp

## ライセンス

MIT License

## 参考

- DMDATA.JP APIリファレンス
  - <https://dmdata.jp/docs/reference/api/v2/>
- 気象庁防災情報XMLフォーマット（VXSE45は「地震火山関連 解説資料」に載っています）
  - <https://xml.kishou.go.jp/tec_material.html>
