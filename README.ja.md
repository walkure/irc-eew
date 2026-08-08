# irc-eew // EEW Slack bot

## なにこれ

Weathernews(WNI)の緊急地震速報(<http://weathernews.jp/quake/html/urgentquake.html>)を受信して
Slackへ流す為の単純な何か。

※名前(および配布イメージ名)に"irc"が残っているのは、かつてIRCにも通知していた頃の歴史的経緯です。
Goへの移植でIRC対応は落としており、現在はSlackのみに通知します。

## 使い方

config.yaml-distを適当に書き換えて、config.yamlにすれば引数指定しなくても読みに行くよ。
limited-noticeは第一報と最終報を通知します。all-noticeは全部投げる。

Slackへのincoming-webhookによる通知も`all`と`limited`の2つがあります。

WNIのパスワードをべた書きするのが嫌だったらmd5化してpasswd-md5に書けばそれを使うよ。
この時、passwdエントリは無視します。

受信EEW情報を保存するエントリ「logdir:」行をコメントアウトすると、ログを保存しなくなります。

コンテナで動かす場合は以下のような感じで：

`docker run -it --rm  --mount type=bind,source=/usr/local/irc-eew/config,target=/conf,readonly ghcr.io/walkure/irc-eew:1.0.1`

## ちぅい

WNIのサービスは有料(\315/month)。あと、何か時々第一報が届かないことがある。
WNIさん、一つのアカウントで複数ログインしても文句言わないっぽいので、多く通知
したければ複数起動した方が良いかも。

何か時々WNIの通知が遅延することがある。TCPだし仕方ないかも。

WNIのプロトコルがどうしようもなくキモい。
サーバへ接続する際にこちらからGETを送るのに、メッセージがある場合何故かサーバ側からGETを
送ってくる。意味不明。

## 作者

walkure at 3pf.jp

## ライセンス

MIT License

## 参考

- Weathernews(The last 10 seconds)
  - <http://weathernews.jp/quake/html/urgentquake.html>
- EEW電文フォーマット(非公式)
  - <http://eew.mizar.jp/excodeformat>
- EEW Decoder(受信部を書くときに参考にしました)
  - <https://github.com/skubota/EEW-decorder>
