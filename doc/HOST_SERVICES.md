# Exposing host services to a sandbox

The `--host-port` flag exposes a TCP service bound to your Mac's loopback
(`127.0.0.1:<port>`) to a sandbox at the *same* address inside the container.
An agent running in the sandbox can talk to the service using exactly the
configuration it would use on the host — no MCP/client reconfiguration
required.

## Example: Figma MCP

The Figma desktop app exposes an MCP server at `http://127.0.0.1:3845/mcp` on
your Mac. To make it reachable from inside a sandbox:

```sh
sand new --host-port 3845 -a claude
```

Inside the sandbox, point any MCP client at the usual URL:

```
http://127.0.0.1:3845/mcp
```

The flag is repeatable:

```sh
sand new --host-port 3845 --host-port 5173
```

## How it works

Apple's `container` CLI puts each sandbox on a vmnet bridge with its own IP.
Inside the sandbox, `127.0.0.1` is the sandbox itself, not your Mac. The Mac
is the bridge gateway (typically `192.168.64.1`).

For each requested port `--host-port` does two things:

1. The `sand` daemon spawns a TCP forwarder bound to the sandbox's bridge
   gateway IP on the host. The listener forwards to `127.0.0.1:<port>` on the
   host. The listener is scoped to the bridge interface — it is **not** bound
   to `0.0.0.0`, so other machines on your LAN cannot reach it.
2. The daemon installs an `iptables` DNAT rule inside the sandbox that
   rewrites `127.0.0.1:<port>` to `<gateway>:<port>` and a matching
   `MASQUERADE` rule for the return path. `net.ipv4.conf.{all,lo}.route_localnet`
   is set to `1` so the kernel will route the redirected packet out of the
   loopback interface.

Both the forwarder and the iptables rule are torn down when the sandbox
stops or is removed.

## Security notes

`--host-port` is opt-in per port, per sandbox. It does, by design, punch a
hole in the sandbox's network isolation — comparable to `--ssh-agent`
forwarding. Only forward ports you would already trust the sandbox to reach
on your own machine.

## Interaction with `--allowed-domains-file`

The eBPF egress filter installed by `--allowed-domains-file` already permits
RFC1918 destinations (including the bridge gateway IP), so no additional
allowlist entries are required.
