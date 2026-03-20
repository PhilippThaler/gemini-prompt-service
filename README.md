# gemini-prompt-service

A minimal Go-based API server for Google Gemini AI

---

## Quick Start

### 1. Environment Configuration

```env
# Google AI Studio API Key
GEMINI_API_KEY="your_actual_key_here"

# Server Configuration
DEFAULT_PORT=8090
GEMINI_DEFAULT_MODEL="gemini-2.5-flash"
GEMINI_DEFAULT_PROMPT="Write a unique, short poem (under 500 chars). Use a current timestamp to ensure a completely original theme and structure every time."
```

### 2. Docker Compose Deployment
Use this `docker-compose.yml` to pull and run the image from your GitHub Container Registry.

```yaml
services:
  gemini-api:
    image: ghcr.io/philippthaler/gemini-prompt-service:latest
    container_name: gemini-prompt-service
    restart: unless-stopped
    ports:
      - "8090:8090"
    environment:
      - GEMINI_API_KEY=<your-api-key>
      - GEMINI_DEFAULT_MODEL="gemini-3.1-flash-lite-preview"
      - GEMINI_DEFAULT_PROMPT="How many bees are on this planet?"
      - DEFAULT_PORT="8090"
    # Recommended for your PLG-Stack
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### 3. Run the Service
```bash
# Pull and start
docker compose pull && docker compose up -d

# Verify logs
docker compose logs -f
```

---

## API Usage

### Health Check
Used by Docker's internal `HEALTHCHECK` to ensure the service is alive.
```bash
curl http://localhost:8090/health
```

### Generate Random Poem (GET)
```bash
curl http://localhost:8090/new
```

### Custom AI Prompt (POST)
Send a specific instruction to the model.
```bash
curl -X POST http://localhost:8090/new \
     -H "Content-Type: application/json" \
     -d '{
       "prompt": "Tell me a super cool joke.",
       "model": "gemini-2.0-flash"
     }'
```

---
*Built with Go 1.26 and Alpine 3.21*
