# Network Filtering

`sand` can restrict container egress with DNS-based filtering by passing an allowed-domains file:

```sh
sand new --allowed-domains-file allowed-domains.txt
```

Before using `--allowed-domains-file`, install the custom init image and BPFFS-enabled kernel:

```sh
sand install-ebpf-support
```

The filtering is implemented at the kernel layer with eBPF support. Use it when you want to limit which domains a sandboxed agent can reach while it works in the cloned workspace.

You can set `--allowed-domains-file` as a default flag value in `~/.sand.yaml` or a project `.sand.yaml`. See [Configuration](CONFIGURATION.md) for how config files are merged.
