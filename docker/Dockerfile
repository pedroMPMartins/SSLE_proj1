# Stage 1: Build static binary
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/service ./cmd/${SERVICE}

# Stage 2: Minimal runtime image
FROM alpine:3.20

WORKDIR /app
COPY --from=builder /app/service /usr/local/bin/service

ENTRYPOINT ["/usr/local/bin/service"]
