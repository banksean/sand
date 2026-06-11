#!/usr/bin/env bash
set -e
CONTAINER_PKG_VERSION=1.0.0

if command -v container &> /dev/null; then
	INSTALLED_VERSION=$(container --version 2>&1)
	if [[ "$INSTALLED_VERSION" == *"$CONTAINER_PKG_VERSION"* ]]; then 
		echo "container version $CONTAINER_PKG_VERSION is already installed"
		container system start
		exit 0
	else
		echo "container version $INSTALLED_VERSION does not match expected version $CONTAINER_PKG_VERSION"
	fi
fi

echo "fetching container-$CONTAINER_PKG_VERSION-installer-signed.pkg..." 
curl -L "https://github.com/apple/container/releases/download/$CONTAINER_PKG_VERSION/container-$CONTAINER_PKG_VERSION-installer-signed.pkg" -o /tmp/container-$CONTAINER_PKG_VERSION-installer-signed.pkg

which -s container && echo "stopping container system" && container system stop

sudo installer -pkg /tmp/container-$CONTAINER_PKG_VERSION-installer-signed.pkg -target /
pkgutil --only-files --files com.apple.container-installer
container system start
