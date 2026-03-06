FROM golang:latest AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o harbor-proxy .

FROM registry.suse.com/bci/bci-minimal:15.7

WORKDIR /app

COPY --from=builder /app/harbor-proxy /app/harbor-proxy

EXPOSE 8080

ENTRYPOINT ["/app/harbor-proxy"]
