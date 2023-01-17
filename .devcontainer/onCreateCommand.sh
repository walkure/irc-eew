#set -eux

sudo apk add --no-cache curl bash \
    perl lighttpd \
    perl-cgi-fast \
    perl-http-message \
    perl-file-slurp \
    perl-io-socket-ssl \
    perl-yaml \
    perl-json

LIBDIR="./lib/Earthquake/EEW"
if [ ! -d "${LIBDIR}" ]; then
    mkdir -p "${LIBDIR}"
fi

if [ ! -e "${LIBDIR}/Decoder.pm" ]; then
    /usr/bin/curl -o "${LIBDIR}/Decoder.pm" https://raw.githubusercontent.com/walkure/Earthquake_EEW_Decoder/master/lib/Earthquake/EEW/Decoder.pm
fi
