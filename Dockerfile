# The app's image: goal-tracker built from source, on a slim Debian with
# Chromium for PDF downloads. (The campaign agents' image is
# vetinari/Dockerfile; this one shares its build context and .dockerignore.)
#
#   docker build -t goal-tracker .
#   docker run -p 8080:8080 -v goal-tracker-data:/data \
#     -e GOAL_TRACKER_SIGNIN=password -e GOAL_TRACKER_SESSION_KEY=... \
#     -e GOAL_TRACKER_BASE_URL=https://goals.example.com goal-tracker
#
# Settings are the environment variables in docs/install.md. The database
# lives on the /data volume; back it up as docs/install.md § Backups says.

# Go pinned with vetinari/Dockerfile and go.mod. Generated templ and sqlc code
# is committed, so the build needs no generators. modernc's SQLite is pure Go,
# so the binary is static.
FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Only the Go source: nothing git ignores (a dev .env, local databases) can
# reach the image.
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/goal-tracker ./cmd/goal-tracker

FROM debian:bookworm-slim
# chromium renders PDF downloads (GOAL_TRACKER_CHROME); without it they fall
# back to the print page. ca-certificates for OIDC and the directory sync;
# sqlite3 for online backups through `docker exec` (docs/install.md).
RUN apt-get update \
  && apt-get install -y --no-install-recommends chromium ca-certificates sqlite3 \
  && rm -rf /var/lib/apt/lists/*
# A real, writable home: Debian's Chromium won't start without one.
RUN useradd --system --uid 10001 --create-home --home-dir /home/goal-tracker --shell /usr/sbin/nologin goal-tracker \
  && mkdir /data && chown goal-tracker:goal-tracker /data
COPY --from=build /out/goal-tracker /usr/local/bin/goal-tracker

# Root-owned working directory, so nothing (a Chromium core dump included)
# lands outside /data.
WORKDIR /
USER goal-tracker
ENV GOAL_TRACKER_ADDR=:8080 \
    GOAL_TRACKER_DB=/data/goal-tracker.db \
    GOAL_TRACKER_CHROME=/usr/bin/chromium
VOLUME /data
EXPOSE 8080
# The runtime has no curl, so the binary checks its own /healthz on the
# loopback, at GOAL_TRACKER_ADDR's port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/goal-tracker", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/goal-tracker"]
