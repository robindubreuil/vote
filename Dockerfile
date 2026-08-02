# D16: base images pinned to immutable manifest-list digests so a tag
# re-point (e.g. golang:1.24-alpine silently moving to a new minor, or
# a registry compromise substituting a malicious image) can't change
# what we pull. The digests below are manifest-list digests (one entry
# per CPU arch) so buildx multi-platform builds still resolve to the
# right variant per platform. Dependabot's `docker` ecosystem bumps the
# tags + digests together on a weekly cadence — when it does, verify
# the changelog of the bumped tag before merging.
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS backend-builder
# D25: no apk/git here — go.mod has no `replace`, no private modules,
# no VCS directives, so `go mod download` resolves via the proxy +
# GOSUMDB and never invokes git. GIT_COMMIT arrives as a build-arg from
# the host checkout, not from inside the stage.
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

FROM node:25-alpine@sha256:bdf2cca6fe3dabd014ea60163eca3f0f7015fbd5c7ee1b0e9ccb4ced6eb02ef4 AS frontend-builder
WORKDIR /build/frontend
# .git/ is excluded from the build context (.dockerignore), so
# gen-version.js would fall back to "unknown". Surface the CI build-args
# as the VOTE_* env vars it prefers, so the footer shows the real
# commit instead of "unknown".
ARG VERSION=dev
ARG BUILD_TIME=""
ARG GIT_COMMIT=""
ARG GIT_FULL_COMMIT=""
ENV VOTE_VERSION=$VERSION \
    VOTE_BUILD_DATE=$BUILD_TIME \
    VOTE_GIT_COMMIT=$GIT_COMMIT \
    VOTE_GIT_FULL_COMMIT=$GIT_FULL_COMMIT
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

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
