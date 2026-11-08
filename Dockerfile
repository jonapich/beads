# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

# Builds and tests bd with nothing but Docker on the host. Use docker-bake.hcl
# as the entry point rather than calling this file directly.
#
# Supply chain: the frontend and base image are pinned by digest, Go modules
# are verified against go.sum and the checksum database, Debian packages come
# from Debian's signed archive, and the Dolt CLI is pinned by sha256. Nothing
# here may fetch from the original project's infrastructure.

FROM golang:1.27.3-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS base
ENV CGO_ENABLED=1 \
    GOFLAGS=-tags=gms_pure_go \
    GOTOOLCHAIN=local \
    GOPROXY=https://proxy.golang.org \
    GOSUMDB=sum.golang.org
WORKDIR /src

FROM base AS windows-toolchain
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc-mingw-w64-x86-64 \
    && rm -rf /var/lib/apt/lists/*

FROM windows-toolchain AS build-windows-amd64
ENV GOOS=windows \
    GOARCH=amd64 \
    CC=x86_64-w64-mingw32-gcc
COPY . .
ARG BD_COMMIT=""
ARG BD_BRANCH=""
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    build="$(printf %.9s "$BD_COMMIT")"; \
    go build -trimpath \
      -ldflags "-s -w -X main.Build=${build:-dev} -X main.Commit=$BD_COMMIT -X main.Branch=$BD_BRANCH -buildmode=exe" \
      -o /out/bd.exe ./cmd/bd \
    && bash scripts/verify-cgo.sh /out/bd.exe

FROM scratch AS windows-amd64
COPY --from=build-windows-amd64 /out/ /

FROM base AS guard
COPY . .
RUN bash scripts/fork/check-upstream-refs.sh

# Tests need the Dolt CLI and must not run as root: several exercise file
# permissions that root bypasses. Keep the Dolt version in step with the
# embedded engine in go.mod.
FROM base AS test
ADD --checksum=sha256:a8d4c2e2e94b85fd3592cb80748044f846ccfd03d25a536ed1f754baf7da6401 \
    https://github.com/dolthub/dolt/releases/download/v2.6.0/dolt-linux-amd64.tar.gz /tmp/dolt.tar.gz
RUN apt-get update \
    && apt-get install -y --no-install-recommends lsof \
    && rm -rf /var/lib/apt/lists/* \
    && tar -xzf /tmp/dolt.tar.gz -C /tmp \
    && install -m 0755 /tmp/dolt-linux-amd64/bin/dolt /usr/local/bin/dolt \
    && rm -rf /tmp/dolt.tar.gz /tmp/dolt-linux-amd64 \
    && useradd --create-home --uid 1000 tester \
    && chown tester:tester /src
USER tester
COPY --chown=tester:tester . .
# A Windows checkout stores symlinks as small text files. Restore the ones the
# docs tests expect.
RUN grep -rlx --include=CLAUDE.md 'AGENTS.md' . | while read -r f; do ln -sf AGENTS.md "$f"; done
ARG BD_TEST_ARGS=""
RUN --mount=type=cache,id=gomod-test,target=/go/pkg/mod,uid=1000,gid=1000 \
    --mount=type=cache,id=gobuild-test,target=/home/tester/.cache/go-build,uid=1000,gid=1000 \
    bash scripts/test.sh $BD_TEST_ARGS
