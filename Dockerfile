# Dockerfile

FROM golang:1.26

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

ENV VERSION = $(shell cat VERSION)

RUN go build -ldflags "-X config.Version=${VERSION}" -o mcphe main.go

# Start mcphe with config.yaml and plugins directory mountable
CMD ["/app/mcphe", "./config/config.yaml"]
