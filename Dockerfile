FROM golang:1.24-alpine3.21 AS builder
WORKDIR /app
COPY . .
RUN go build -o time-automation .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/time-automation .
COPY --from=builder /app/.env.example .env

# Set timezone to Europe/Berlin
ENV TZ=Europe/Berlin
RUN ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime

CMD ["./time-automation"]
