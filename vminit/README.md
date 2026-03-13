# Custom vm init image for eBPF

This only partially works, so it's not baked into the rest of `sand` yet.

Its purpose is to set up ePBF during the vm init phase, so that `sand` can set up DNS allowlists and block agents from accessing other domains at the OS kernel level.

This image is meant to be used with `apple/container`'s  `--init-image` flag like so:

```sh
container run -i -t --name my-container --init-image ghcr.io/banksean/sand/custom-init:latest ghcr.io/banksean/sand/base:latest
```

It wraps Apple's default [`vminitd`](https://github.com/apple/containerization/tree/main/vminitd) image with its own [`cmd/initwrapper`](cmd/initwrapper/) process. `initwrapper` starts a separate DNS proxy sidecar process before Exec'ing out to Apple's `vminitd` process, where it continues as it would without this custom init image.

The DNS proxy sidecar is implemented in [`cmd/dnsproxy`](cmd/dnsproxy/). It performs a lot of unnatural, ugly acts in order to mount /sys/fs/bpf and writer /etc/resolv.conf etc at the appropriate times during the vm init sequence.

This code barely works when it works at all. In particular, the pre-resolved domain queries rarely cover all the addresses. E.g. pinging an allowlisted domain from inside a guest container will often resolve to an IP address that wasn't in the pre-resolved set for that domain. So it fails in the overly-restrictive direction.

You can get an idea of what the pre-resolved queries cover by running this command from your host OS after your container starts up:
```sh
> container logs --boot my-container
...
2026/03/12 23:52:30 network ready, pre-resolving domains...
2026/03/12 23:52:30 pre-resolved api.github.com -> 140.82.116.6
2026/03/12 23:52:30 pre-resolved github.com -> 140.82.116.3
2026/03/12 23:52:30 pre-resolved www.github.com -> 140.82.116.3
2026/03/12 23:52:30 pre-resolved registry.npmjs.org -> 104.16.10.34
2026/03/12 23:52:30 pre-resolved registry.npmjs.org -> 104.16.4.34
2026/03/12 23:52:30 pre-resolved registry.npmjs.org -> 104.16.3.34
2026/03/12 23:52:30 pre-resolved registry.npmjs.org -> 104.16.7.34
```

I'm considering scrapping this approach and forking apple's vminit, so that we don't have to use a sidecar at all. That's a whole other can of worms though.