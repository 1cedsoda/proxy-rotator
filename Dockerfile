FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY proxy-gateway/go.mod proxy-gateway/go.sum ./
RUN go mod download

COPY proxy-gateway/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o proxy-gateway-server ./cmd/proxy-gateway-server


FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/proxy-gateway-server /proxy-gateway-server

EXPOSE 8100

ENTRYPOINT ["/proxy-gateway-server"]
CMD ["/data/config/config.toml"]
