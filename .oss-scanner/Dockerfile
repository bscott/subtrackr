# Build environment for Anthropic's OSS Scanner. The scanner supplies the SubTrackr checkout as the build
# context and expects it, built, at /src. Network is available only while this file builds.
FROM golang:1.24-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    CGO_ENABLED=1 \
    GOTOOLCHAIN=local

# gcc + libsqlite3-dev: mattn/go-sqlite3 (via gorm.io/driver/sqlite) is cgo. sqlite3 CLI: inspect the database.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates build-essential git pkg-config sqlite3 libsqlite3-dev curl jq procps \
    && rm -rf /var/lib/apt/lists/*

COPY . /src
WORKDIR /src

# Fetch every module (build and test) while the network is up.
RUN go mod download && go mod verify

# Build both binaries the way the maintainers do (see Makefile / Dockerfile), keeping DWARF debug info.
RUN go build -o /usr/local/bin/subtrackr ./cmd/server \
    && go build -o /usr/local/bin/subtrackr-mcp ./cmd/mcp

# Compile and run the Go test suite (about a minute) so test dependencies and build caches are warm.
# A failing baseline test must not abort the image build.
RUN go vet ./... || echo "WARNING: go vet reported problems"
RUN go test -count=1 -timeout 15m ./... || echo "WARNING: go test reported failures"

# The Playwright suite under tests/ drives a browser and needs npm + Chromium; it is not installed here.
# Use the Go tests and direct HTTP requests against the running server instead (see threat_model.md).

# Nothing may be fetched after this point: new reproducers and patches must build offline.
ENV GOPROXY=off \
    GOSUMDB=off \
    GOFLAGS=-mod=mod

# Runtime defaults. The server resolves templates/ and web/ relative to its working directory, so start it from /src.
RUN mkdir -p /src/data
ENV DATABASE_PATH=/src/data/subtrackr.db \
    PORT=8080 \
    GIN_MODE=release

CMD ["bash"]
