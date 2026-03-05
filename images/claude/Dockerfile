FROM ghcr.io/banksean/sand/base:latest

ARG CLAUDE_CODE_VERSION=latest

RUN apk add --no-cache nodejs npm

ENV NPM_CONFIG_PREFIX=/usr/local/share/npm-global
ENV PATH=$PATH:/usr/local/share/npm-global/bin

RUN mkdir -p /usr/local/share/npm-global /root/.claude /commandhistory && \
  touch /commandhistory/.bash_history && \
  chown -R root /commandhistory

RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}
