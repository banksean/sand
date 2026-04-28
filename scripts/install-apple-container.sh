#!/usr/bin/env bash
set -e
CONTAINER_PKG_VERSION=0.12.0

echo "fetching container-$CONTAINER_PKG_VERSION-installer-signed.pkg..." 
curl -L "https://github.com/apple/container/releases/download/$CONTAINER_PKG_VERSION/container-$CONTAINER_PKG_VERSION-installer-signed.pkg" -o /tmp/container-$CONTAINER_PKG_VERSION-installer-signed.pkg

which -s container && echo "stopping container system" && container system stop

sudo installer -pkg /tmp/container-$CONTAINER_PKG_VERSION-installer-signed.pkg -target /
pkgutil --only-files --files com.apple.container-installer
container system start