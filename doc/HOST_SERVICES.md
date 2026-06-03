# Exposing host services to a sandbox

The `--host-port` flag on `sand new` (repeatable) makes a TCP service bound to
your Mac's loopback (`127.0.0.1:<port>`) reachable from inside a sandbox at
`http://host.sand:<port>/`.

## Example: Figma MCP

The Figma desktop app exposes an MCP server at `http://127.0.0.1:3845/mcp` on
your Mac. Expose it to a sandbox:

```sh
sand new --host-port 3845 -a claude
```

Inside the sandbox:

```sh
curl -v http://host.sand:3845/mcp
```

Configure the agent's MCP client to use the same URL. For Claude Code:

```sh
claude mcp add --transport http figma http://host.sand:3845/mcp
```

Multiple ports:

```sh
sand new --host-port 3845 --host-port 5173
```

## How it works

Apple's `container` CLI puts each sandbox on a vmnet bridge with its own IP.
From inside the sandbox, `127.0.0.1` is the sandbox itself, not your Mac;
your Mac is the bridge gateway (typically `192.168.64.1`).

For each requested port `--host-port` does the following:

1. The `sand` daemon starts a TCP forwarder listening on the sandbox's
   bridge gateway IP. The listener is scoped to the bridge interface only
   (not `0.0.0.0`), so other machines on your LAN cannot reach it. The
   forwarder targets `127.0.0.1:<port>` on the Mac.
2. The forwarder is HTTP-aware: when a client speaks HTTP/1.x it rewrites
   the `Host:` header to `127.0.0.1:<port>` on the way to the upstream.
   That keeps servers that validate `Host` (Figma's MCP among them) happy
   without any client-side workarounds. Non-HTTP traffic is forwarded as
   plain TCP.
3. An `/etc/hosts` entry is added inside the sandbox mapping `host.sand`
   to the gateway IP, so `http://host.sand:<port>/` Just Works.
4. Optionally, a best-effort `iptables` DNAT is installed inside the
   sandbox so `127.0.0.1:<port>` is transparently redirected to
   `host.sand:<port>`. Apple's container runtime currently does not grant
   `CAP_NET_ADMIN`, so this step usually fails silently; the host.sand
   path remains the supported entry point.

Forwarders, the `/etc/hosts` entry, and any iptables rules are torn down
when the sandbox stops or is removed.

## Security

`--host-port` is opt-in per port, per sandbox. It does, by design, punch a
hole in the sandbox's network isolation — comparable in trust to
`--ssh-agent` forwarding. Only forward ports you would already trust the
sandbox to reach on your machine.

## Interaction with `--allowed-domains-file`

The eBPF egress filter installed by `--allowed-domains-file` already permits
RFC1918 destinations (including the bridge gateway IP), so no additional
allowlist entries are required.
