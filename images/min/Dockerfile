FROM ghcr.io/banksean/sand/base:latest

# TODO: Generate this keypair outside the container and mount it in as a secret.
# As-is, these keys are baked into the image.
RUN ssh-keygen -t ed25519 -C "root@sand" -f /etc/ssh/ssh_host_ed25519_key
