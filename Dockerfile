FROM golang:1.24-alpine3.21 AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o time-automation .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /app/time-automation .
COPY --from=builder /app/.env.example .env

# Set timezone to Europe/Berlin
ENV TZ=Europe/Berlin
RUN ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8077/health || exit 1

CMD ["./time-automation"]
