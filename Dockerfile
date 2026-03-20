FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/site .

FROM alpine:3.21.3
WORKDIR /app
RUN apk add --no-cache tzdata=2025a-r0 ca-certificates=20241121-r1

# Security setup
RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir -p /app/data && \
    chown -R appuser:appgroup /app

COPY --from=builder /app/site .
RUN chown appuser:appgroup /app/site

USER appuser

# This matches your Go code variable name
ENV DEFAULT_PORT=8090
EXPOSE 8090

ENTRYPOINT ["./gemini-api"]
