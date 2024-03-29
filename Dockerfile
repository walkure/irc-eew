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

RUN addgroup -g 1000 -S nonroot \
	&& adduser -u 1000 -S nonroot -G nonroot
USER nonroot

WORKDIR /usr/src/irc-eew
COPY --chown=nonroot:nonroot *.pm *.pl /usr/src/irc-eew/
ADD --chown=nonroot:nonroot https://raw.githubusercontent.com/walkure/Earthquake_EEW_Decoder/master/lib/Earthquake/EEW/Decoder.pm /usr/src/irc-eew/lib/Earthquake/EEW/

# If you set `TZ=Asia/Tokyo`, system cannot recognize it(and use UTC tz)
#  because `/usr/share/zoneinfo/Asia/Tokyo` not exists.
# Also no need to set `TZ=JST-9`.

ENTRYPOINT [ "perl","./irc-eew.pl"]
CMD ["/conf/config.yaml"]
