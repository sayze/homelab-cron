FROM golang:1.24-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/cron ./cmd/cron

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /usr/local/bin/homelab-cron-api
COPY --from=build /out/cron /usr/local/bin/homelab-cron

EXPOSE 8080

# No default ENTRYPOINT/CMD: this image holds both binaries, and the
# caller picks which one to run (Nomad's task config.command, or
# docker-compose's command: below).
