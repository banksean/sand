FROM docker.io/library/ubuntu
RUN echo 'Acquire::http::Proxy "http://192.168.64.1:3142";' > /etc/apt/apt.conf.d/01proxy
RUN apt-get update && \
    apt-get install -y --no-install-recommends --reinstall ca-certificates \
    curl \
    openssh-client \
    openssh-server \
    sudo \
    systemctl \
    tmux \
    unzip \
    wget \
    zsh && \
    rm -rf /var/lib/apt/lists/*

ENV DEVCONTAINER=true

RUN mkdir -p /workspace
WORKDIR /workspace
USER root

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
ARG SAND_CLI_VERSION=v0.0.10
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