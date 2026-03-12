FROM ghcr.io/apple/containerization/vminit:0.27.0 AS base

FROM ghcr.io/apple/containerization/vminit:0.27.0
COPY --from=base /sbin/vminitd /sbin/vminitd.real
COPY initwrapper /sbin/vminitd
