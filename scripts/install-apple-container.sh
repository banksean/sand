#!/usr/bin/env bash
set -e

curl -L "https://github.com/apple/container/releases/download/0.8.0/container-installer-signed.pkg" -o container-installer-signed.pkg
sudo installer -pkg container-installer-signed.pkg -target /
pkgutil --only-files --files com.apple.container-installer
