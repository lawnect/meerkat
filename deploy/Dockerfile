# syntax=docker/dockerfile:1
FROM golang:1.26.3-bookworm AS go-build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/connector ./cmd/connector && \
    CGO_ENABLED=0 go build -trimpath -o /out/outlet ./cmd/outlet && \
    CGO_ENABLED=0 go build -trimpath -o /out/demo-service ./cmd/demo-service && \
    CGO_ENABLED=0 go build -trimpath -o /out/poc-probe ./cmd/poc-probe

FROM debian:bookworm-slim AS rathole-download
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip && rm -rf /var/lib/apt/lists/*
# Stable v0.5.0 provides Noise on both platforms. ARM64's embedded build does
# not include TLS; the control API still uses Go TLS on both architectures.
RUN case "$TARGETARCH" in \
      amd64) target=x86_64-unknown-linux-gnu ;; \
      arm64) target=aarch64-unknown-linux-musl ;; \
      *) exit 1 ;; \
    esac && \
    curl --fail --location --retry 3 -o /tmp/rathole.zip "https://github.com/rathole-org/rathole/releases/download/v0.5.0/rathole-${target}.zip" && \
    unzip /tmp/rathole.zip -d /out && chmod 0755 /out/rathole && \
    /out/rathole --version
RUN curl --fail --location --retry 3 -o /out/RATHOLE-LICENSE \
    https://raw.githubusercontent.com/rathole-org/rathole/v0.5.0/LICENSE

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=go-build /out/ /usr/local/bin/
COPY --from=rathole-download /out/rathole /usr/local/bin/rathole
COPY --from=rathole-download /out/RATHOLE-LICENSE /usr/share/doc/rathole/LICENSE
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/connector"]
