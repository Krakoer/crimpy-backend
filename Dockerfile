FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o server ./cmd/api

FROM alpine:3.20
RUN adduser -D -u 10001 crimpy && mkdir -p /logs && chown crimpy /logs
WORKDIR /app
COPY --from=builder /app/server .
USER crimpy
EXPOSE 3000
ENTRYPOINT [ "/app/server" ]
