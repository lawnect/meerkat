# syntax=docker/dockerfile:1
# Every base image is pinned by tag and by the digest of its multi-platform
# index, so a rebuild of a tag can't change what a release contains. Bump the
# tag and the digest together.
FROM node:22-bookworm-slim@sha256:be979e5523c76fa4d5ca4ebd3099078f220aa47ec39c48ad5b5df9314debd6eb AS ui-build
WORKDIR /src
COPY package.json package-lock.json ./
COPY tsconfig.json ./
RUN npm ci
COPY web ./web
RUN npm run check && npm run build

# Keep the tag equal to the `toolchain` line in go.mod: the CI workflow refuses
# a difference and runs govulncheck with the Go version read from this line.
FROM golang:1.26.11-bookworm@sha256:ea47ee45f1284d60f9cc09cb2c3bb4f508c256b640bd8bb30342ff13d1adcb52 AS go-build
# The release, without the "v"; the publish workflow passes it from the tag.
ARG VERSION=0.0.0-dev
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY --from=ui-build /src/web/dist ./internal/dashboard/assets/site
# One go build for every binary, so their packages compile and link in parallel.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/lawnect/meerkat/internal/contract.Version=${VERSION}" -o /out/ \
    ./cmd/connector ./cmd/outlet ./cmd/outlet-init ./cmd/demo-service

# The digest pins the toolchain so a Docker Hub rebuild of the tag doesn't
# throw away the cached rathole build. Bump it together with the tag.
FROM rust:1.98-bookworm@sha256:93ce27a88655056a51dbdd8f5f2d7ddc071c7b0070fb288a37b5a285fc83971e AS rathole-build
# rathole v0.5.0 is the latest release, and its lockfile pins crates with
# known advisories, snow 0.9.3 among them (RUSTSEC-2024-0011: a forged packet
# can make the tunnel drop). The image builds the v0.5.0 source against
# deploy/rathole/Cargo.lock, which is its lockfile after `cargo update`: the
# same release on the latest compatible dependencies. Only what the tunnel
# uses is built: server, client, Noise and hot reload, without TLS or
# websocket, so OpenSSL isn't linked either.
ARG RATHOLE_COMMIT=ebb764ae53d7ffe4fcb45f83f7563bec5c74199d
RUN git clone --depth 1 --branch v0.5.0 https://github.com/rathole-org/rathole /src && \
    test "$(git -C /src rev-parse HEAD)" = "$RATHOLE_COMMIT"
WORKDIR /src
COPY deploy/rathole/Cargo.lock ./Cargo.lock
RUN cargo build --release --locked --no-default-features --features server,client,noise,hot-reload && \
    mkdir /out && cp target/release/rathole /out/rathole && cp LICENSE /out/RATHOLE-LICENSE && \
    /out/rathole --version | grep -Eq '^Build Version: +0\.5\.0$' && \
    /out/rathole --version | grep -q hot_reload

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /var/lib/meerkat && chown 65532:65532 /var/lib/meerkat
COPY --from=go-build /out/ /usr/local/bin/
COPY --from=rathole-build /out/rathole /usr/local/bin/rathole
COPY --from=rathole-build /out/RATHOLE-LICENSE /usr/share/doc/rathole/LICENSE
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/connector"]
