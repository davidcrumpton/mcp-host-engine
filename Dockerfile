# Dockerfile

FROM golang:1.26

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -o mcphe main.go

# Start mcphe with config.yaml and plugins directory mountable
CMD ["/app/mcphe", "./config/config.yaml"]
