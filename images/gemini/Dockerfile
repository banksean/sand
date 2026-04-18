FROM ghcr.io/banksean/sand/base:latest

ARG GEMINI_CLI_VERSION=latest

RUN apk add --no-cache nodejs npm

ENV NPM_CONFIG_PREFIX=/usr/local/share/npm-global
ENV PATH=$PATH:/usr/local/share/npm-global/bin

RUN npm install -g @google/gemini-cli@${GEMINI_CLI_VERSION}
