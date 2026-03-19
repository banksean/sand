# Custom init image and vmlinux binary for DNS filtering

Note: This only partially works at the moment, so it's not integrated into the rest of `sand` yet.

This directory contains code to build a custom init image that enables *kernel-level* egress filtering using eBPF and a DNS proxy.

Also note that this is for an *init* image, only to be used with the `--init-image` flag as shown below. This type of image is distinct from container images like the ones you would typically specify using positional arguments when invoking apple's `container` commands in most use cases.

## Usage

You will first need to [build a linux kernel that has BPFFS enabled](#custom-kernel-build-instructions), and set it as `container`'s system kernel. This is a one-time step that doesn't need to be repeated every time you instantiate a container using this init image, but this init image will not work without that kernel binary. See [custom kernel build instructions](#custom-kernel-build-instructions) below. 

Once you have built the kernel and configured `container` to use it, you'll need to build this custom init image like so:
```sh
make image
```

If that succeeds, you can try out the DNS allowlist filtering using `apple/container`'s `--init-image` and `--dns` flags like so:

```sh
container run -i -t \
  --name my-container \
  --init-image ghcr.io/banksean/sand/custom-init:latest \ # this image
  --dns 127.0.0.1 \ # the dns proxy created by cmd/initwrapper
  ghcr.io/banksean/sand/base:latest # the guest container image
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

## Implementation

The `custom-init` image replaces Apple's default [`vminitd`](https://github.com/apple/containerization/tree/main/vminitd) image with its own [`cmd/initwrapper`](cmd/initwrapper/) process. `initwrapper` starts a separate DNS proxy sidecar process before Exec'ing out to Apple's `vminitd` process, where it continues as it would without this custom init image.

The DNS proxy sidecar is implemented in [`cmd/dnsproxy`](cmd/dnsproxy/). 

The domain [allowlist](./allowed-domains.txt) is still baked into the `custom-init` image, so it's not yet very useful beyond demonstration purposes.  Ideally, one could specify this list via `sand` configuration settings without having to rebuild the `custom-init` image, but that isn't implemented yet.

## Custom kernel build instructions

The default kernel build that Apple's containerization framework uses does not include the BTF metadata required to mount BPFFS (`/sys/fs/bpf`), which `cmd/dnsproxy` needs to exist when it starts up, in order to load the egress filter code into the kernel.

So I forked the apple/containerization repo (https://github.com/banksean/containerization) and modified its linux kernel build flags so that BPFFS *is* available. With these flags you can build a BPFFS-enabled vmlinux binary like so:  

```sh
git clone git@github.com:banksean/containerization.git
cd containerization/kernel
make kernel-build
```

`make kernel-build` can take a few minutes, but once it finishes there should be a new kernel binary in `./vmlinux`

You then have to tell the `container` system to use this kernel with the following command: 

```sh
container system kernel set --arch=arm64 --binary <path to vmlinux file>
```

If you run into problems trying to use this kernel image, you can always revert to the default kernel provided by apple/container with this command:

```sh
container system kernel set --default
```