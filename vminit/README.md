# Custom vm init image for eBPF

This only partially works, so it's not integrated into the rest of `sand` yet.

Its purpose is to apply DNS allowlisting *at the kernel level*, via ePBF.
To make this work, we have to change our vm's init process:
- create and apply the eBPF configuration
- spawn a local DNS proxy process (in the VM's namespace, not the container's) at 127.0.0.1:53
- configure the container settings to use that nameserver address in its resolv.conf 

You can try this image out using `apple/container`'s `--init-image` and `--dns` flags like so:

```sh
container run -i -t \
  --name my-container \
  --init-image ghcr.io/banksean/sand/custom-init:latest \
  --dns 127.0.0.1 \ # the dns proxy created by cmd/initwrapper
  ghcr.io/banksean/sand/base:latest
```

When you get a shell prompt, try looking up some domain names that are either in the allowlist or not to see the difference:

```sh
/workspace # nslookup api.github.com
Server:         127.0.0.1:53
Address:        127.0.0.1:53

Non-authoritative answer:

Non-authoritative answer:
Name:   api.github.com
Address: 140.82.116.6

/workspace # nslookup reddit.com
Server:         127.0.0.1:53
Address:        127.0.0.1:53

** server can't find reddit.com: REFUSED

** server can't find reddit.com: REFUSED
```

The `custom-init` image wraps Apple's default [`vminitd`](https://github.com/apple/containerization/tree/main/vminitd) image with its own [`cmd/initwrapper`](cmd/initwrapper/) process. `initwrapper` starts a separate DNS proxy sidecar process before Exec'ing out to Apple's `vminitd` process, where it continues as it would without this custom init image.

The DNS proxy sidecar is implemented in [`cmd/dnsproxy`](cmd/dnsproxy/). 

The domain [allowlist](./allowed-domains.txt) is still baked into the `custom-init` image, so it's not yet very useful beyond demonstration purposes.  Ideally, one could specify this list via `sand` configuration settings without having to rebuild the `custom-init` image.
