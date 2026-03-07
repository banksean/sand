# TODO: tune this to work more seamlessly with macOS, user settings etc
FROM ghcr.io/banksean/sand/base:latest

ARG CLAUDE_CODE_VERSION=latest

RUN apk add --no-cache \
  github-cli \
  gnupg \
  make \
  nodejs \
  npm

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
