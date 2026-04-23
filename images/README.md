# Sandbox Container Images

[`base`](./base) contains the shared Alpine foundation used by the default and agent-specific images.
- common apk packages (zsh, git, ssh, iptables, etc.)
- zsh + oh-my-posh shell setup
- GitHub SSH known_hosts configuration
- sshd_config
- [mise](https://mise.jdx.dev/) auto-detection and initialization script (enables cross-container tool, dependency and build caching) 
- a [cross-compiled `sand` linux cli](../cmd/sand/main_linux.go) binary for use from inside the container

Images that extend `base` (you can use these with `sand new --image`):
- **[claude](./claude/)** — Claude Code
- **[codex](./codex/)** — OpenAI Codex
- **[default](./default/)** — no coding agent
- **[gemini](./gemini/)** - Google Gemini
- **[opencode](./opencode/)** - OpenCode

## Building and debugging images locally

To build images locally:
```sh
# build all images (in parallel)
make -j all

# or individual images
make opencode
```

To use these locally-built images instead of images from ghcr.io, specify `-i` or `--image` with `:local` tags like so:

```sh
sand new -i default:local
sand new --image opencode:local
```

