FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -tags wasm2go,nodynamic -ldflags="-s -w -X main.version=$VERSION" -o /out/fileshare-backend ./cmd/server/

FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:1314fb30ec37592d5845e5a160d522c0e680972aa534549940b8d73bde88481a AS vendor
WORKDIR /src
COPY scripts/build-vendor.sh scripts/
COPY scripts/vendor scripts/vendor
RUN sh scripts/build-vendor.sh /out/js

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2
COPY --from=build /out/fileshare-backend /app/fileshare-backend
COPY assets/web /app/web
COPY --from=vendor /out/js /app/web/js
ENV FS_DATA_DIR=/data FS_LOG_DIR=/data/logs FS_PORT=27182
EXPOSE 27182
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/fileshare-backend", "healthcheck"]
ENTRYPOINT ["/app/fileshare-backend"]
