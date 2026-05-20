FROM ghcr.io/banksean/sand/base:latest

RUN apk add --no-cache \
    curl \
    bash \
    git \
    libc6-compat \
    libstdc++

ENV OPENCODE_VERSION=1.14.48
RUN curl -fsSL https://opencode.ai/install | bash -s -- --version ${OPENCODE_VERSION}