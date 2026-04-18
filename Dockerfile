FROM golang:1.24-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
ARG VERSION=dev
ARG BUILD_TIME=""
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -s -w" \
    -o vote-server ./cmd/server

FROM node:22-alpine AS frontend-builder
WORKDIR /build
COPY shared/ shared/
COPY scripts/ scripts/

WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --ignore-scripts
COPY frontend/ .
RUN npm run build

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -H -u 1000 vote

COPY --from=backend-builder /build/vote-server /usr/bin/vote-server
COPY --from=frontend-builder /build/frontend/dist /usr/share/vote/www

USER vote
EXPOSE 8080

ENTRYPOINT ["vote-server"]
