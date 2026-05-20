#!/usr/bin/env bash -x
set -euo pipefail

# Stop and uninstall everything

if command -v sand &> /dev/null; then
	echo "Removing sandboxes"
	sand rm -af
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
if [ -f "~/Library/Application\ Support/Sand" ]; then
	chmod -R u+w ~/Library/Application\ Support/Sand
	rm -rf ~/Library/Application\ Support/Sand
fi

# remove ~/.ssh/config Include line that may have been added by sand
sed -i '' '\|\Include /Users/[^/][^/]*/.config/sand/ssh_config|d' ~/.ssh/config

rm -rf /tmp/sand

# remove ~/.ssh/config Include line that may have been added by sand
sed -i '' '\|\Include /Users/[^/][^/]*/.config/sand/ssh_config|d' ~/.ssh/config

# if we ever install any launchd services:
#launchctl bootout gui/$(id -u) com.banksean.sand 2>/dev/null || true

# uninstall container
sudo /usr/local/bin/uninstall-container.sh -k || true


