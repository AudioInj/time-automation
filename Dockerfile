FROM golang:1.27.3-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o time-automation .

FROM alpine:3.24
WORKDIR /app
COPY --from=builder /app/time-automation .
COPY browser/ ./browser/
# Optional CA mount supports CI environments with an inspecting HTTPS proxy.
RUN --mount=type=secret,id=proxy_ca \
    if [ -f /run/secrets/proxy_ca ]; then \
      export SSL_CERT_FILE=/run/secrets/proxy_ca NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca; \
    fi; \
    apk --no-cache add ca-certificates tzdata wget nodejs npm chromium && \
    npm --prefix browser ci --omit=dev --ignore-scripts && \
    ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime
ENV TZ=Europe/Berlin \
    CHROMIUM_PATH=/usr/bin/chromium \
    BROWSER_HELPER=/app/browser/client.mjs
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8077/health || exit 1
CMD ["./time-automation"]
