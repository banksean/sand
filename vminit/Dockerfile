FROM --platform=linux/arm64 docker.io/library/alpine:latest AS go-builder
ARG GO_VERSION=1.25.1
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  wget "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  tar -C /usr/local -xzf "go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  rm "go${GO_VERSION}.linux-${ARCH}.tar.gz"

ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/root/go
ENV PATH=$PATH:/root/go/bin
COPY ./ /build/
RUN cd /build && \
    ls -la && \
    go build -o ./initwrapper ./cmd/initwrapper && \
    go build -o ./dnsproxy ./cmd/dnsproxy

FROM --platform=linux/arm64 docker.io/library/alpine:latest AS bpf-builder
RUN apk add --no-cache clang llvm libbpf-dev linux-headers iproute2-tc

COPY egress_filter.c /build/
RUN clang -O2 -target bpf -D__TARGET_ARCH_arm64 \
    -I/usr/include/aarch64-linux-musl \
    -c /build/egress_filter.c \
    -o /build/egress_filter.o

FROM ghcr.io/apple/containerization/vminit:0.27.0 AS base

FROM ghcr.io/apple/containerization/vminit:0.27.0
COPY --from=base /sbin/vminitd /sbin/vminitd.real
COPY --from=bpf-builder /build/egress_filter.o /sbin/egress_filter.o
COPY --from=go-builder /build/initwrapper /sbin/vminitd
COPY --from=go-builder /build/dnsproxy /sbin/dns-allowlist-proxy
COPY allowed-domains.txt /etc/sandbox/allowed-domains.txt

