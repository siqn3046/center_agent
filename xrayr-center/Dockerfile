# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /center ./cmd/center

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /center /usr/local/bin/center
ENV CENTER_LISTEN=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/center"]
