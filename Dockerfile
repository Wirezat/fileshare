FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/fileshare-backend ./cmd/server/

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fileshare-backend /app/fileshare-backend
COPY assets/web /app/web
ENV FS_DATA_DIR=/data FS_LOG_DIR=/data/logs FS_PORT=27182
EXPOSE 27182
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/fileshare-backend", "healthcheck"]
ENTRYPOINT ["/app/fileshare-backend"]
