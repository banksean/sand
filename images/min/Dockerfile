FROM alpine

ARG TZ
ENV TZ="$TZ"

# Install basic development tools and iptables/ipset
RUN apk add --no-cache \
  bash \
  less \
  git \
  procps \
  sudo \
  fzf \
  zsh \
  man-db \
  unzip \
  iptables \
  ipset \
  iproute2 \
  jq \
  nano \
  vim \
  openssh-client \
  openssh-server

# Set `DEVCONTAINER` environment variable to help with orientation
ENV DEVCONTAINER=true

# Create workspace and config directories and set permissions
RUN mkdir -p /workspace && \
  chown -R root:root /workspace

# Generate some SSH host keys so that the SSH server can start.
# TODO: Generate this keypair outside the container and mount it in as a secret.
# As-is, the ssh-keygen below will run once at image creation time, and the keys will
# be baked into the image, which is ...not ideal.
RUN ssh-keygen -t ed25519 -C "root@sand" -f /etc/ssh/ssh_host_ed25519_key

# Configure SSH for GitHub authentication
RUN mkdir -p /root/.ssh && \
  chmod 700 /root/.ssh && \
  ssh-keyscan github.com >> /root/.ssh/known_hosts && \
  echo "Host github.com\n\tPubkeyAuthentication yes" > /root/.ssh/config && \
  chmod 600 /root/.ssh/config

WORKDIR /workspace

# Set default user to root
USER root

# Set the default shell to zsh rather than sh
ENV SHELL=/bin/zsh

# Set the default editor and visual
ENV EDITOR=nano
ENV VISUAL=nano

# Default powerline10k theme
ARG ZSH_IN_DOCKER_VERSION=1.2.1
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
  -p git \
  -p fzf \
  -a "source /usr/share/doc/fzf/examples/key-bindings.zsh" \
  -a "source /usr/share/doc/fzf/examples/completion.zsh" \
  -a "export PROMPT_COMMAND='history -a' && export HISTFILE=/commandhistory/.bash_history" \
  -a '[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh'

# Install oh-my-posh
RUN curl -s https://ohmyposh.dev/install.sh | bash -s -- -d /usr/local/bin