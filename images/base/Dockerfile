FROM docker.io/library/alpine

ARG TZ
ENV TZ="$TZ"

RUN apk add --no-cache \
  bash \
  curl \
  delta \
  fzf \
  git \
  htop \
  iproute2 \
  ipset \
  iptables \
  jq \
  less \
  libgcc \
  libstdc++ \
  man-db \
  nano \
  openssh-client \
  openssh-server \
  procps \
  ripgrep \
  sudo \
  tmux \
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

# Install innie (container-side sand cli binary)
ARG SAND_CLI_VERSION=v0.0.4
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  wget "https://github.com/banksean/sand/releases/download/${SAND_CLI_VERSION}/innie_linux_${ARCH}.tar.gz" && \
  tar -C /usr/local/bin -xzf "innie_linux_${ARCH}.tar.gz" && \
  mv /usr/local/bin/innie /usr/local/bin/sand && \
  rm "innie_linux_${ARCH}.tar.gz"

ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a '[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh'

RUN curl -s https://ohmyposh.dev/install.sh | bash -s -- -d /usr/local/bin
