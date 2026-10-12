ARG GO_VERSION=1.26.0
ARG BASE_IMAGE=ubuntu:22.04
FROM golang:${GO_VERSION}-trixie AS toolchain
RUN CGO_ENABLED=0 go install github.com/go-task/task/v3/cmd/task@v3.53.1

FROM ${BASE_IMAGE}
RUN apt-get update && apt-get install --yes --no-install-recommends \
    ca-certificates curl file git python3 gcc g++ cmake make libgl1-mesa-dev xorg-dev \
    libwayland-dev libxkbcommon-dev desktop-file-utils patchelf \
    && rm -rf /var/lib/apt/lists/*
COPY --from=toolchain /usr/local/go /usr/local/go
COPY --from=toolchain /go/bin/task /usr/local/bin/task
ENV PATH=/usr/local/go/bin:$PATH CGO_ENABLED=1
WORKDIR /workspace
