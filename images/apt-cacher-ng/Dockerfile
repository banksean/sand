FROM ubuntu:latest

# Install the proxy
RUN apt-get update && apt-get install -y apt-cacher-ng && rm -rf /var/lib/apt/lists/*

# Fix permissions for the cache directory 
# and redirect logs to stdout so you can see them via 'container logs'
RUN mkdir -p /var/cache/apt-cacher-ng && \
    chown -R apt-cacher-ng:apt-cacher-ng /var/cache/apt-cacher-ng

# Port 3142 is the default for apt-cacher-ng
EXPOSE 3142

# Run the daemon in the foreground so the container doesn't exit
CMD ["/usr/sbin/apt-cacher-ng", "-c", "/etc/apt-cacher-ng", "ForeGround=1"]
