FROM golang:1.24-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
    ARG VERSION=dev
    ARG BUILD_TIME=""
    ARG GIT_COMMIT=""
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT} -s -w" \
    -o vote-server ./cmd/server
# Pre-create the FHS data dir (0700) so the distroless runtime stage can
# COPY it in with nonroot ownership — distroless has no shell to mkdir.
RUN mkdir -p -m 0700 /build/varlib

FROM node:22-alpine AS frontend-builder
WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts
COPY frontend/ .
RUN npm run build

# D10: distroless runtime image. static-debian12 ships only CA certs +
# zoneinfo + /etc/nsswitch.conf — no shell, no package manager, no libc.
# The server binary is built CGO_ENABLED=0 so it is fully static and runs
# unmodified under distroless. The smaller surface means fewer CVEs and a
# much smaller image. The :nonroot variant runs as UID 65532.
#
# Because distroless has no shell, HEALTHCHECK cannot use wget/curl; the
# server binary self-probes via `vote-server --health` (an HTTP GET to
# /livez on loopback) so the probe needs no extra tooling in the image.
FROM gcr.io/distroless/static-debian12:nonroot

# Distroless has no shell, so the FHS data dir cannot be mkdir'd at image
# build time here. It is created in the builder stage (which has a shell)
# and copied in with nonroot ownership so the process can persist its
# stats counters/history. The VOLUME directive preserves ownership into
# anonymous volumes, so `-v` mounts and anonymous volumes both work.
COPY --from=backend-builder --chown=nonroot:nonroot --chmod=0700 /build/varlib/ /var/lib/vote/
VOLUME /var/lib/vote
ENV VOTE_DATA_DIR=/var/lib/vote

COPY --from=backend-builder --chown=nonroot:nonroot /build/vote-server /usr/bin/vote-server
COPY --from=frontend-builder --chown=nonroot:nonroot /build/frontend/dist /usr/share/vote/www

USER nonroot:nonroot
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/usr/bin/vote-server", "--health"]

ENTRYPOINT ["/usr/bin/vote-server"]
