# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
# Runtime image for a precompiled, statically linked ARTEX binary.
# Build binaries through build.sh or use Dockerfile.source for a source build.
FROM node:22-bookworm-slim@sha256:e9f565d32859f0cbfb52125eafe8be39390f24dac302dc3cdf566e8ba3ab21ee
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates python3 python3-pip python3-venv ripgrep curl wget vim git jq unzip \
    dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && rm -rf /var/lib/apt/lists/*
COPY tools/browser/package*.json /opt/artex-browser/
RUN cd /opt/artex-browser && npm ci --ignore-scripts --omit=dev \
    && PLAYWRIGHT_BROWSERS_PATH=/ms-playwright ./node_modules/.bin/playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/opt/artex-browser/node_modules/.bin:${PATH}" \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    ARTEX_KEY_DIR=/app/private \
    HOME=/app/data/home
# Install browsers into a location available to the unprivileged runtime user.
RUN groupadd --gid 10001 artex && useradd --uid 10001 --gid 10001 --no-create-home artex \
    && mkdir -p /app/data/home /app/private /app/skills \
    && chown -R artex:artex /app /ms-playwright
WORKDIR /app
COPY --chmod=0555 dist/${TARGETARCH}/artex /app/artex
COPY --chmod=0555 start.sh /app/start.sh
COPY --chown=10001:10001 skills/ /app/skills/
USER 10001:10001
VOLUME ["/app/data", "/app/private"]
EXPOSE 8787
ENTRYPOINT ["/app/artex"]
# The recording proxy is private to tools in this container, never published.
CMD ["-addr", ":8787", "-proxy", "127.0.0.1:8788"]
