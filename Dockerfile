# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" \
    go build -trimpath -ldflags="-s -w" -o /out/recoil .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S recoil \
    && adduser -S -D -H -u 10001 -G recoil -s /sbin/nologin recoil \
    && mkdir -p /data \
    && chown -R recoil:recoil /data

COPY --from=build /out/recoil /usr/local/bin/recoil

USER recoil
EXPOSE 8787
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/recoil"]
CMD ["relay", "serve", "--addr", ":8787", "--data", "/data"]
