# --- Build stage: full Go toolchain ---
FROM golang:1.27-alpine AS build
WORKDIR /src

# Download dependencies first so this layer is cached until go.mod changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0 produces a static binary with no C library dependency.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- Runtime stage: just the binary on a small base image ---
FROM alpine:3.22
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --retries=10 \
  CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["api"]
