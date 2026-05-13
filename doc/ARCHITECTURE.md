# Architecture and Trade-Offs

`sand` runs entirely on your workstation: no remote hosting and no third-party access to your files. The trade-off is that it is bounded by your local hardware resources.

`sand` is agent-agnostic, which means it cannot exploit deep agent-specific features. The upside is that sandbox lifecycles are independent of agent session lifecycles, and sandboxes are equally useful for manual coding without any agent.

`sand` achieves speed partly by doing less: it does not automate `git` or `tmux` workflows beyond what is needed for sandbox management. You can create a new sandbox from an existing sandbox as easily as branching in git.

## Implementation choices

- Isolation model: [Apple Containerization](https://github.com/apple/containerization)
  - hardware isolation via Apple Silicon
  - low memory overhead and fast start times
  - kernel based on [Kata](https://katacontainers.io/)
  - used via Apple's [`container` CLI](https://github.com/apple/container), currently requiring version `0.12.3`
  - supported on macOS 26 and up
- Filesystem:
  - base container image is minimal, with dynamic provisioning based on which agent you use
  - agent workspaces are mounted at `/app` from an APFS copy-on-write clone
  - the clone must live on the same APFS volume as the original project directory
  - host filesystem access is limited to the copy-on-write clone directory
- Execution interface:
  - CLI fast exec path for one-shot commands
  - session path for interactive use
  - host daemon handles sandbox lifecycle management
  - host CLI is a thin wrapper around daemon IPC calls
  - container-side `sand` CLI reaches the same daemon through container-to-host networking

## Open design areas

- Lifecycle and pooling strategy: `sand` currently spawns containers on demand, with no pooling. You can stop and start sandbox containers manually, but `sand` does not yet monitor container activity or utilization to manage that automatically.
- Network topology: per-container isolated network vs. shared host network vs. a managed bridge is still constrained by what macOS and Apple Containerization support.
