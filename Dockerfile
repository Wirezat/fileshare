FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/fileshare-backend ./cmd/server/

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/fileshare-backend /app/fileshare-backend
COPY assets/web /app/web
ENV FS_DATA_DIR=/data FS_LOG_DIR=/data/logs FS_PORT=27182
EXPOSE 27182
ENTRYPOINT ["/app/fileshare-backend"]
