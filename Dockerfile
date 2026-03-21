# Stage 1: Build Go binary
FROM golang:1.26-bookworm AS go-builder

WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download

# Copy frontend into embed directory before building
COPY frontend/ ./web/static/
COPY server/ ./
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: Runtime with Python + ffmpeg
FROM python:3.11-slim-bookworm

RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg && \
    rm -rf /var/lib/apt/lists/*

COPY requirements.txt /opt/tc/requirements.txt
RUN pip install --no-cache-dir -r /opt/tc/requirements.txt

COPY main.py parser.py mixer.py tts.py /opt/tc/

COPY --from=go-builder /app /usr/local/bin/server

RUN mkdir -p /data
ENV DB_PATH=/data/jobs.db
ENV PYTHON_BIN=python3
ENV PYTHON_DIR=/opt/tc
ENV PORT=8080

EXPOSE 8080

CMD ["server"]
