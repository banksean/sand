# TODO: tune this to work more seamlessly with macOS, user settings etc
FROM ghcr.io/banksean/sand/base:latest

ARG CLAUDE_CODE_VERSION=latest

RUN apk add --no-cache \
  github-cli \
  gnupg \
  make \
  nodejs \
  npm

RUN mkdir -p /commandhistory && \
  touch /commandhistory/.bash_history && \
  chown -R root /commandhistory
