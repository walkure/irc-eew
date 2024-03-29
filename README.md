# irc-eew // EEW-IRC/Slack bot

## About

Receive EEW(Earthquake Early Warning) from Weathernews(WNI)'s paid service via TCP
and Post to IRC/Slack channels.

## How to use

-Write your configucation at config.yaml about WNI EEW and your IRC server/channel or Your Slack incoming-webhooks.
-Start this script.

*No flood control while posting messages to IRC server.

For Docker: `docker run -it --rm  --mount type=bind,source=/usr/local/irc-eew/config,target=/conf,readonly ghcr.io/walkure/irc-eew:latest`

You can run EEW viewer. for details: `./HTML/`.

## Dependencies

- Earthquake::EEW::Decoder
  - <https://github.com/Magistol/EEWDecoder>

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
