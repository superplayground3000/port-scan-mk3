# port-scan-mk3 — Ubuntu 26.04 base image.
# All source files are copied into the image; .git is excluded via .dockerignore.
FROM ubuntu:26.04

# Pin the Go toolchain to match go.mod (go 1.24.x).
ARG GO_VERSION=1.24.4

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/usr/local/go/bin:/root/go/bin:$PATH \
    GOPATH=/root/go

# Install the Go toolchain and CA certificates (needed for module downloads / TLS).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && arch="$(dpkg --print-architecture)" \
    && case "$arch" in \
         amd64) goarch=amd64 ;; \
         arm64) goarch=arm64 ;; \
         *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
       esac \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz" -o /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz \
    && apt-get purge -y curl \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy all source files into the image (.git and other noise excluded by .dockerignore).
COPY . .

# Build all commands under cmd/ into /usr/local/bin.
RUN go build -o /usr/local/bin/ ./cmd/...

# Default to the primary CLI entrypoint.
ENTRYPOINT ["port-scan"]
CMD ["--help"]
