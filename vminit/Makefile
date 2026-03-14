INIT_IMAGE_NAME := ghcr.io/banksean/sand/custom-init

.PHONY: image

image:
	container build -t $(INIT_IMAGE_NAME):latest ./

clang:
		apk add --no-cache clang llvm libbpf-dev linux-headers iproute2-tc

dnsproxy: clang
	clang -O2 -target bpf -D__TARGET_ARCH_arm64 \
    -I/usr/include/aarch64-linux-musl \
    -c egress_filter.c \
    -o /sbin/egress_filter.o