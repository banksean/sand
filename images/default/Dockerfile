# TODO: tune this to work more seamlessly with macOS, user settings etc
FROM ghcr.io/banksean/sand/base:latest

ARG CLAUDE_CODE_VERSION=latest
# TODO: get this from a build arg, passed in from boxer, based on whatever is in the cloned dir's go.mod
ARG GO_VERSION=1.25.1

RUN apk add --no-cache \
  github-cli \
  gnupg \
  make \
  nodejs \
  npm

# Install Go toolchain
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  wget "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  tar -C /usr/local -xzf "go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  rm "go${GO_VERSION}.linux-${ARCH}.tar.gz"

ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/root/go
ENV PATH=$PATH:/root/go/bin

ENV NPM_CONFIG_PREFIX=/usr/local/share/npm-global
ENV PATH=$PATH:/usr/local/share/npm-global/bin

RUN mkdir -p /usr/local/share/npm-global /root/.claude /commandhistory && \
  touch /commandhistory/.bash_history && \
  chown -R root /commandhistory

ADD sand-sketch /usr/local/bin/sand-sketch
RUN chmod +x /usr/local/bin/sand-sketch

# Install Claude Code
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}

# TODO: use a pre-built binary instead of building here
RUN git clone https://github.com/boldsoftware/sketch.git /tmp/sketch && \
  cd /tmp/sketch && \
  make all && \
  mv embedded/sketch-linux/sketch-linux-arm64 /usr/local/bin/sketch
