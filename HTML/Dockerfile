FROM alpine:3.17.0
RUN apk add --no-cache \
    ca-certificates \
    perl \
    perl-cgi-fast \
    perl-file-slurp \
    lighttpd \
    tzdata && \
    cp /usr/share/zoneinfo/Asia/Tokyo /etc/localtime && \
    apk del tzdata

RUN addgroup -g 1000 -S nonroot \
    && adduser -u 1000 -S nonroot -G nonroot
USER nonroot

COPY lighttpd.conf /etc/lighttpd/

WORKDIR /var/www/docker/fastcgi/
COPY --chown=nonroot:nonroot *.pl /var/www/docker/fastcgi/
ADD --chown=nonroot:nonroot https://raw.githubusercontent.com/walkure/Earthquake_EEW_Decoder/master/lib/Earthquake/EEW/Decoder.pm /var/www/docker/fastcgi/lib/Earthquake/EEW/

EXPOSE 80

# https://redmine.lighttpd.net/boards/2/topics/8382
ENTRYPOINT /usr/sbin/lighttpd -D -f /etc/lighttpd/lighttpd.conf 3>&1
