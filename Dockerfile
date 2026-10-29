# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build statically linked binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o audit ./cmd/audit

# Final production stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

COPY --from=builder /app/server .
COPY --from=builder /app/audit .
COPY --from=builder /app/migrations ./migrations

EXPOSE 8080

CMD ["./server"]
