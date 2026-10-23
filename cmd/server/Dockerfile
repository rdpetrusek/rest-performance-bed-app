# The ReST Bed dashboard's API (cmd/server): a single static Go binary on
# distroless. The Angular app is its own image (ui/Dockerfile); see
# cmd/server/compose.yaml for how this is run.
#
# Build context is the repo root (it needs go.mod and internal/);
# Dockerfile.dockerignore next to this file limits it to just those.

# Built on the build machine's own platform and cross-compiled, so multi-arch
# builds (amd64 + arm64) don't run the Go toolchain under emulation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/server ./cmd/server

# Root, not :nonroot - the existing restbed_data volume is root-owned.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
VOLUME ["/data"]
EXPOSE 8000
ENTRYPOINT ["/server"]
