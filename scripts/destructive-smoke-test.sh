#!/usr/bin/env bash -x
set -euo pipefail

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

if command -v sand &> /dev/null; then
	echo "Removing sandboxes"
	sand rm -a
	echo "Stopping daemon"
	sandd stop

	if brew list "banksean/tap/sand" &>/dev/null; then
		echo "Uninstalling brew package"
		brew uninstall banksean/tap/sand
	else
		echo "Removing non-brew binary installation"
		rm $(which sand)
		rm $(which sandd)
	fi
fi

rm -rf ~/.config/sand
chmod -R u+w ~/Library/Application\ Support/Sand
rm -rf ~/Library/Application\ Support/Sand

if container image inspect ghcr.io/banksean/sand/default &>/dev/null; then 
	echo "Removing ghcr.io/banksean/sand/default from local image registry"
	container image rm ghcr.io/banksean/sand/default
fi

# Install sand and sandd from source
go install ./cmd/...

# Re-evaluate where the sand binary is located in $PATH
# Without this, the script will continue to try to use the
# binary installed by brew instead of the one we just built.
hash -r
which sand
which sandd

# Execute some commands that should work without any issues
sand --version
sand build-info
sand ls

# Create a new sandox and exit back to this smoke test.
# Use the `script` command here to avoid tty errors.
echo "exit" | script -q /dev/null sand new -i default:local smoke
sand ls

# TODO: Automate verification for the output of these commands
sand exec smoke ls
sand exec smoke whoami
# Cold cache for both go toolchain and build artifacts
time sand exec smoke zsh -c "go test ./..."
# Warm chache, should be much faster this time
time sand exec smoke zsh -c "go test ./..."

# Try to use the packaged sand innie binary from the default image
sand exec smoke sand --version
sand exec smoke sand build-info

# Now try to build and use the sand innie binary built from this checkout
sand exec smoke zsh -c "go build ./cmd/..."
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
chmod -R u+w ~/Library/Application\ Support/Sand
rm -rf ~/Library/Application\ Support/Sand
