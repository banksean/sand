FROM docker.io/library/alpine

ARG TZ
ENV TZ="$TZ"

RUN apk add --no-cache \
  bash \
  curl \
  fzf \
  git \
  iproute2 \
  ipset \
  iptables \
  jq \
  less \
  man-db \
  nano \
  openssh-client \
  openssh-server \
  procps \
  sudo \
  unzip \
  vim \
  zsh

ENV DEVCONTAINER=true

RUN mkdir -p /workspace
WORKDIR /workspace
USER root

ENV SHELL=/bin/zsh
ENV EDITOR=nano
ENV VISUAL=nano

# Configure SSH for GitHub authentication
RUN mkdir -p /root/.ssh && \
  chmod 700 /root/.ssh && \
  ssh-keyscan github.com >> /root/.ssh/known_hosts && \
  printf "Host github.com\n\tPubkeyAuthentication yes\n" > /root/.ssh/config && \
  chmod 600 /root/.ssh/config

ADD sshd_config /etc/ssh/sshd_config

ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a '[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh'

RUN curl -s https://ohmyposh.dev/install.sh | bash -s -- -d /usr/local/bin
