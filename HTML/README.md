# eew-view

A viewer of EEW Files retrieved by `irc-eew`.

## Usage

`docker run -it --rm -p 8080:80 --mount type=bind,source=/a/path/to/eewlog,target=/eewlog,readonly ghcr.io/walkure/eew-view:latest`

## Configurations

You can configure by setting environment variables.

|name|default|description|
|----|-------|-----------|
|`EEW_DATA_DIR`|`/eewlog/`|path to EEW files|
|`EEW_PATH_BASE`|`./`|URI path base|
|`EEW_VIEWER`|`eew-show`| name of EEW viewer|
