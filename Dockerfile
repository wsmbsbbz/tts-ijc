# Stage 1: Build Go binary
FROM golang:1.26-bookworm AS go-builder

WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: Extract telegram-bot-api binary
FROM aiogram/telegram-bot-api:latest AS tgapi

# Stage 3: Runtime with Python + ffmpeg + telegram-bot-api
FROM python:3.11-slim-bookworm

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ffmpeg \
        libssl3 \
        zlib1g \
        libstdc++6 \
        libatomic1 && \
    rm -rf /var/lib/apt/lists/*

COPY cli/requirements.txt /opt/tc/requirements.txt
RUN pip install --no-cache-dir -r /opt/tc/requirements.txt

COPY cli/ /opt/tc/

COPY --from=go-builder /app /usr/local/bin/server
COPY --from=tgapi /usr/local/bin/telegram-bot-api /usr/local/bin/telegram-bot-api

COPY frontend/ /opt/frontend/
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

RUN mkdir -p /data /var/lib/telegram-bot-api
ENV DB_PATH=/data/jobs.db
ENV PYTHON_BIN=python3
ENV PYTHON_DIR=/opt/tc
ENV FRONTEND_DIR=/opt/frontend
ENV PORT=8080

EXPOSE 8080

CMD ["entrypoint.sh"]
