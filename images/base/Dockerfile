FROM docker.io/library/alpine

ARG TZ
ENV TZ="$TZ"

RUN apk add --no-cache \
  bash \
  curl \
  delta \
  fzf \
  git \
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

# Install Go toolchain so that we can build and install the sand cli "innie" version for the container.
ARG GO_VERSION=1.25.1
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  wget "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  tar -C /usr/local -xzf "go${GO_VERSION}.linux-${ARCH}.tar.gz" && \
  rm "go${GO_VERSION}.linux-${ARCH}.tar.gz"

ENV PATH=$PATH:/usr/local/go/bin
ENV GOPATH=/root/go
ENV PATH=$PATH:/root/go/bin

RUN git clone https://github.com/banksean/sand.git /tmp/sand && \
  cd /tmp/sand && \
  go build -o ./innie ./cmd/sand/... && \
  mv ./innie /usr/local/bin/sand

ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a '[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh'

RUN curl -s https://ohmyposh.dev/install.sh | bash -s -- -d /usr/local/bin
