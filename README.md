# irc-eew // EEW Slack bot

## About

Receive EEW(Earthquake Early Warning) from Weathernews(WNI)'s paid service via TCP
and Post to Slack channels.

## How to use

-Write your configucation at config.yaml about WNI EEW and your Slack incoming-webhooks.
-Start this script.

For Docker: `docker run -it --rm  --mount type=bind,source=/usr/local/irc-eew/config,target=/conf,readonly ghcr.io/walkure/irc-eew:latest`

## EEW Viewer

A read-only web viewer for archived EEW telegrams (see `logdir:` above).

`docker run -it --rm -p 8080:8080 --mount type=bind,source=/a/path/to/eewlog,target=/eewlog,readonly ghcr.io/walkure/eew-view:latest`

Configure via environment variables:

|name|default|description|
|----|-------|-----------|
|`EEW_DATA_DIR`|`/eewlog/`|path to EEW files|
|`EEW_PATH_BASE`|`./`|URI path base|
|`EEW_VIEWER`|`eew-show`|name of the detail-view route|
|`EEW_LISTEN_ADDR`|`:8080`|address to listen on|

## FYI

*Written in Japanese only

- Weathernews
  - <http://weathernews.jp/quake/html/urgentquake.html>
- EEW Message Code format(Un-official)
  - <http://eew.mizar.jp/excodeformat>
- EEW Decoder(receiver sample)
  - <https://github.com/skubota/EEW-decorder>

## Author

 walkure at 3pf.jp

## License

 MIT License
