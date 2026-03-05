# Sandbox Container Images

## Structure

`base` contains the shared Alpine foundation used by all images except `opencode`:
- common apk packages (zsh, git, ssh, iptables, etc.)
- zsh + oh-my-posh shell setup
- GitHub SSH known_hosts configuration
- sshd_config

Images that extend `base` (`ghcr.io/banksean/sand/base:latest`):
- **claude** — adds Claude Code
- **codex** — adds OpenAI Codex
- **default** — adds Claude Code, Go toolchain, github-cli, and sketch

`opencode` is standalone (uses `frolvlad/alpine-glibc` instead of `alpine` due to glibc requirements) and maintains its own copy of the common setup.

## Build order

Build and push `base` before any of the other images (except `opencode`):

```sh
docker buildx build --push -t ghcr.io/banksean/sand/base:latest images/base/
```
