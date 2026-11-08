# syntax=docker/dockerfile:1

FROM golang:1.27-bookworm AS build

WORKDIR /src
ENV CGO_ENABLED=0 \
    GOFLAGS=-mod=vendor \
    GOPROXY=off

COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/mob3 ./cmd/mob3

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="MOB³" \
      org.opencontainers.image.description="A small terminal web browser" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/mob3net/mob3"

COPY --from=build /out/mob3 /usr/local/bin/mob3
COPY LICENSE THIRD_PARTY_NOTICES /usr/share/doc/mob3/

ENTRYPOINT ["/usr/local/bin/mob3"]
