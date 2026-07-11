FROM docker.io/library/debian:bookworm-slim

ARG TZ
ENV TZ="$TZ"

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    build-essential \
    ca-certificates \
    curl \
    doas \
    fzf \
    git \
    gh \
    gnupg \
    htop \
    iproute2 \
    ipset \
    iptables \
    jq \
    less \
    libgcc-s1 \
    libstdc++6 \
    man-db \
    make \
    nano \
    nodejs \
    npm \
    openssh-client \
    openssh-server \
    procps \
    ripgrep \
    sudo \
    tmux \
    unzip \
    vim \
    wget \
    zsh \
  && rm -rf /var/lib/apt/lists/*

RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  curl -fsSL -o /tmp/atch.tgz "https://github.com/mobydeck/atch/releases/latest/download/atch-linux-${ARCH}.tgz" && \
  tar -C /usr/local/bin -xzf /tmp/atch.tgz atch && \
  chmod +x /usr/local/bin/atch && \
  rm /tmp/atch.tgz

RUN ARCH=$(uname -m) && \
  case "$ARCH" in \
    x86_64) DELTA_ARCH="x86_64-unknown-linux-gnu" ;; \
    aarch64) DELTA_ARCH="aarch64-unknown-linux-gnu" ;; \
    *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;; \
  esac && \
  DELTA_URL=$(curl -fsSL https://api.github.com/repos/dandavison/delta/releases/latest | \
    jq -r --arg arch "$DELTA_ARCH" '.assets[] | select(.name | test($arch + ".*\\.tar\\.gz$")) | .browser_download_url' | \
    head -n 1) && \
  test -n "$DELTA_URL" && \
  curl -fsSL -o /tmp/delta.tgz "$DELTA_URL" && \
  mkdir -p /tmp/delta && \
  tar -C /tmp/delta --strip-components=1 -xzf /tmp/delta.tgz && \
  mv /tmp/delta/delta /usr/local/bin/delta && \
  chmod +x /usr/local/bin/delta && \
  rm -rf /tmp/delta /tmp/delta.tgz

ENV DEVCONTAINER=true

RUN mkdir -p /workspace
WORKDIR /workspace
USER root

RUN mkdir -p /commandhistory && \
  touch /commandhistory/.bash_history && \
  chown -R root /commandhistory

ENV SHELL=/bin/zsh
ENV EDITOR=nano
ENV VISUAL=nano

# Configure SSH for GitHub authentication
# TODO: move this to a container hook so it runs for the actual user name instead of root.
# For now, we just copy these to the user home dir in defaultContainerHook after we create
# the user.
RUN mkdir -p /root/.ssh && \
  chmod 700 /root/.ssh && \
  ssh-keyscan github.com >> /root/.ssh/known_hosts && \
  printf "Host github.com\n\tPubkeyAuthentication yes\n" > /root/.ssh/config && \
  chmod 600 /root/.ssh/config

ADD sshd_config /etc/ssh/sshd_config

# Install innie (container-side sand cli binary)
ARG SAND_CLI_VERSION
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
  wget "https://github.com/banksean/sand/releases/download/${SAND_CLI_VERSION}/innie_linux_${ARCH}.tar.gz" && \
  tar -C /usr/local/bin -xzf "innie_linux_${ARCH}.tar.gz" && \
  mv /usr/local/bin/innie /usr/local/bin/sand && \
  rm "innie_linux_${ARCH}.tar.gz"

# This is common in Docker to avoid interactive password prompts.
RUN echo '%sudo ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/sudo && \
  echo '%wheel ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/wheel && \
  chmod 0440 /etc/sudoers.d/sudo /etc/sudoers.d/wheel

RUN echo 'permit nopass :sudo' > /etc/doas.conf

ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a '[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh'

RUN curl -s https://ohmyposh.dev/install.sh | bash -s -- -d /usr/local/bin

# Install mise
RUN curl https://mise.jdx.dev/install.sh | sh
RUN cp /root/.local/bin/mise /usr/local/bin/mise

ENV PATH="/root/.local/share/mise/bin:$PATH"

WORKDIR /app
COPY mise.sh /usr/local/bin/mise.sh
RUN chmod +x /usr/local/bin/mise.sh
RUN echo "# Initializing..." > /etc/shared_env.sh
RUN echo "source /etc/shared_env.sh" >> /etc/bash.bashrc

RUN if [ -x "$(command -v zsh)" ]; then \
    echo "source /etc/shared_env.sh" >> /etc/zsh/zshenv; \
    echo "source /etc/shared_env.sh" >> /etc/zsh/zshrc; \
    fi

ENV BASH_ENV=/etc/shared_env.sh
ENV ENV=/etc/shared_env.sh
