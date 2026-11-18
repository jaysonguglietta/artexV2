# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# Runtime for precompiled static API binaries; executable tools live in workers.
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS trust
RUN mkdir -p /out/rootfs/app/data/home /out/rootfs/app/private /out/rootfs/tmp \
    && chmod 1777 /out/rootfs/tmp && chmod 0700 /out/rootfs/app/private

FROM scratch
ARG TARGETARCH
ARG ARTEX_BUILD_VERSION=dev
ARG ARTEX_BUILD_COMMIT=unknown
LABEL org.opencontainers.image.source="https://github.com/jaysonguglietta/artexV2" \
    org.opencontainers.image.version="${ARTEX_BUILD_VERSION}" org.opencontainers.image.revision="${ARTEX_BUILD_COMMIT}"
COPY --from=trust --chown=10001:10001 /out/rootfs/ /
COPY --from=trust /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=trust /usr/share/zoneinfo/ /usr/share/zoneinfo/
COPY --chmod=0555 dist/${TARGETARCH}/artex /app/artex
COPY --chown=10001:10001 skills/ /app/skills/
ENV HOME=/app/data/home ARTEX_KEY_DIR=/app/private
WORKDIR /app
USER 10001:10001
VOLUME ["/app/data", "/app/private"]
EXPOSE 8787
ENTRYPOINT ["/app/artex"]
CMD ["-addr", ":8787", "-proxy", "127.0.0.1:8788"]
