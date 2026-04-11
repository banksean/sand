#!/usr/bin/env bash -x

# DANGER: this smoke test will: 
# - remove all sandboxes
# - remove sand configuration data
# - uninstall sand
# - remove local copies of container images used by sand
#
# To be more deterministic, we want to start this test from a known state that resembles
# a realistic first-time use case. Because it deletes everything first, this test can
# take a while to run as it downloads everything all over again.
# 
# Assumptions:
# - apple/container is already installed on the host machine

# Stop and uninstall everything
sand rm -a
sandd stop

brew uninstall banksean/tap/sand

rm $(which sand)
rm $(which sandd)

rm -rf ~/.config/sand
rm -rf ~/Library/Application\ Support/Sand

container image rm ghcr.io/banksean/sand/default

# Install sand and sandd from source
go install ./cmd/...

# Execute some commands that should work without any issues
sand --version
sand build-info
sand ls

# Create a new sandox and exit back to this script
echo "exit" | sand new smoke
sand ls

# TODO: Automate verification for the output of these commands
sand exec smoke ls
sand exec smoke whoami
sand exec smoke apk add go
sand exec smoke go test ./...

# Try to use the packaged sand innie binary from the default image
sand exec smoke sand --verison
sand exec smoke sand build-info

# Now try to build and use the sand innie binary built from this checkout
sand exec smoke go build ./cmd/...
sand exec smoke ./sand --version
sand exec smoke ./sand build-info

# This is kind of annoying since it doesn't automatically close the window.
# TODO: figure out how to automatically close the window.
sand vsc smoke

# Try connecting to the container via ssh. Should Just Work and avoid TOFU prompt, warnings etc.
# TODO: get the domain extension from apple/container instead of assuming it's .test.
ssh smoke.test whoami

# Clean everything up 
sand rm -a
sandd stop
rm $(which sand)
rm $(which sandd)
rm -rf ~/.config/sand
rm -rf ~/Library/Application\ Support/Sand
