FROM golang:1.26.0-trixie AS go-toolchain
FROM fyneio/fyne-cross-images:windows@sha256:5663afcd79d447e56c57226c3a06cc895fd83a4aa99d2d1974cf0338e9d08f36

RUN rm -rf /usr/local/go
COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV GO_VERSION=1.26.0

RUN apt-get update && apt-get install --yes --no-install-recommends python3 cmake make binutils gcc-mingw-w64-x86-64-posix g++-mingw-w64-x86-64-posix \
    && rm -rf /var/lib/apt/lists/*

ENV PROVIEW_OCR_CC=x86_64-w64-mingw32-gcc-posix \
    PROVIEW_OCR_CXX=x86_64-w64-mingw32-g++-posix
