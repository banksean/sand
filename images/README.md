# Sandbox Container Image

[`base`](./base) contains the shared Alpine foundation used by all new sandboxes.
- common apk packages (zsh, git, ssh, iptables, make, gh, etc.)
- zsh + oh-my-posh shell setup
- GitHub SSH known_hosts configuration
- sshd_config
- [mise](https://mise.jdx.dev/) auto-detection and initialization script (enables cross-container tool, dependency and build caching)
- a cross-compiled `sand` linux cli binary for use from inside the container

Agent CLIs are not baked into separate images. `sand new --agent ...` uses the base image, then installs the selected agent during the container's first-start hooks.

## Building and debugging locally

To build the base image locally:

```sh
make
```

To use a locally-built base image instead of the image from ghcr.io, specify `-i` or `--image`:

```sh
sand new --image base:local
sand new --agent codex --image base:local
```
