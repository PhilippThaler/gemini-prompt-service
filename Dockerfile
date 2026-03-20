FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/gemini-api .

FROM alpine:3.21.3
WORKDIR /app
RUN apk add --no-cache \
		tzdata=2026a-r0 \ 
		ca-certificates=20250911-r0

# Security setup
RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir -p /app/data && \
    chown -R appuser:appgroup /app

COPY --from=builder /app/gemini-api .
RUN chown appuser:appgroup /app/gemini-api

USER appuser

# This matches your Go code variable name
ENV DEFAULT_PORT=8090
EXPOSE 8090

ENTRYPOINT ["./gemini-api"]

# Add Healthcheck
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${DEFAULT_PORT}/health || exit 1
