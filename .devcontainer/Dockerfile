FROM alpine:3.17.0
RUN apk add --no-cache \
    curl bash \
    perl \
    lighttpd \
    perl-cgi-fast \
    perl-http-message \
    perl-file-slurp \
    perl-io-socket-ssl \
    perl-yaml \
    perl-json

# avoid 'exec /bin/bash bash' and failure '/bin/bash: /bin/bash: cannot execute binary file'
RUN SWITCHED_TO_BASH=true bash -c "$(curl -fsSL "https://raw.githubusercontent.com/microsoft/vscode-dev-containers/main/script-library/common-alpine.sh")"

WORKDIR /workspaces
USER vscode
