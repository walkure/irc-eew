FROM alpine:3.17.0
RUN apk add --no-cache \
	ca-certificates \
	perl \
	perl-http-message \
	perl-io-socket-ssl \
	perl-yaml \
	perl-json \
	tzdata && \
	cp /usr/share/zoneinfo/Asia/Tokyo /etc/localtime && \
	apk del tzdata

WORKDIR /usr/src/irc-eew
COPY *.pm *.pl /usr/src/irc-eew/
ADD https://raw.githubusercontent.com/walkure/Earthquake_EEW_Decoder/master/lib/Earthquake/EEW/Decoder.pm /usr/src/irc-eew/lib/Earthquake/EEW/

# If you set `TZ=Asia/Tokyo`, system cannot recognize it(and use UTC tz)
#  because `/usr/share/zoneinfo/Asia/Tokyo` not exists.
# Also no need to set `TZ=JST-9`.

ENTRYPOINT [ "perl","./irc-eew.pl","/conf/config.yaml" ]
