FROM perl:5.34 as builder

RUN cpanm HTTP::Message@6.44
RUN cpanm IO::Socket::SSL@2.077
RUN cpanm YAML@1.30
RUN cpanm JSON@4.10

FROM perl:5.34-slim

WORKDIR /usr/src/irc-eew
COPY --from=builder /usr/local/lib/perl5 /usr/local/lib/perl5
COPY *.pm *.pl /usr/src/irc-eew/
ADD https://raw.githubusercontent.com/walkure/Earthquake_EEW_Decoder/master/lib/Earthquake/EEW/Decoder.pm /usr/src/irc-eew/lib/Earthquake/EEW/

ENV TZ=Asia/Tokyo
ENTRYPOINT [ "perl","./irc-eew.pl","/conf/config.yaml" ]
