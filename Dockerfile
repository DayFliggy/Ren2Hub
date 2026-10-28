FROM oven/bun:1.4.0@sha256:5ff609364c049b54eb0ff560ec96319729a972078ef2c755d758f0c6ef89c2d6 AS frontend-dependencies

WORKDIR /build/frontend
COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile

FROM node:24-bookworm-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS frontend-builder

WORKDIR /build/frontend
COPY --from=frontend-dependencies /build/frontend/node_modules ./node_modules
COPY ./frontend ./
COPY ./scripts/prepare-frontend-embed.mjs /build/scripts/prepare-frontend-embed.mjs
COPY ./VERSION /build/VERSION
RUN node ./node_modules/vue-tsc/bin/vue-tsc.js -b \
    && VITE_API_MODE=http node ./node_modules/vite/bin/vite.js build \
    && node /build/scripts/prepare-frontend-embed.mjs

FROM golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS builder2
ENV GO111MODULE=on CGO_ENABLED=0 GOWORK=off

ARG APP_VERSION
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
# relaykit and tokenkit are local submodules referenced via replace; their
# go.mod files must be present for go mod download to resolve the main module graph.
ADD relaykit/go.mod ./relaykit/go.mod
ADD tokenkit/go.mod ./tokenkit/go.mod
RUN go mod download

COPY . .
COPY --from=frontend-builder /build/frontend/embed-dist ./frontend/embed-dist
RUN go test -timeout 60s -run '^TestProductionEmbeddedAssetGraph$' .
RUN version="${APP_VERSION:-$(cat VERSION)}" \
    && test -n "$version" \
    && go build -pgo="$(go list -m -f '{{.Dir}}' github.com/Calcium-Ion/moejs)/default.pgo" -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$version'" -o new-api \
    && go version -m new-api | head -n 1 | cut -d' ' -f2 > go-version \
    && grep -Eq '^go[0-9]+\.[0-9]+' go-version

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
# The Go toolchain that built the binary, recorded for release verification:
# read it with
# docker run --rm --entrypoint cat <image> /etc/ren2hub/go-version
COPY --from=builder2 /build/go-version /etc/ren2hub/go-version
COPY LICENSE NOTICE THIRD-PARTY-LICENSES.md /licenses/
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
