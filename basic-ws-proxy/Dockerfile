FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o proxy

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/proxy /app/proxy
EXPOSE 8080
CMD ["./proxy"]