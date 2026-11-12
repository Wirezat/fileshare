FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:30b1e546a9c7fa3b747c2bdba3a0c7bdf3a02ebb4c5b8405d2f948d7e8aac4ec AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/fileshare-backend ./cmd/server/

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2
COPY --from=build /out/fileshare-backend /app/fileshare-backend
COPY assets/web /app/web
ENV FS_DATA_DIR=/data FS_LOG_DIR=/data/logs FS_PORT=27182
EXPOSE 27182
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/fileshare-backend", "healthcheck"]
ENTRYPOINT ["/app/fileshare-backend"]
