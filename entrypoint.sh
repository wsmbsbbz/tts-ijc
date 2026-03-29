#!/bin/bash
set -e

if [ -n "$TELEGRAM_API_ID" ] && [ -n "$TELEGRAM_API_HASH" ]; then
    if ! command -v telegram-bot-api > /dev/null 2>&1; then
        echo "entrypoint: telegram-bot-api binary not found, skipping"
    else
        mkdir -p /var/lib/telegram-bot-api
        telegram-bot-api \
            --api-id="$TELEGRAM_API_ID" \
            --api-hash="$TELEGRAM_API_HASH" \
            --local \
            --dir=/var/lib/telegram-bot-api \
            --http-port=8081 &
        TG_PID=$!
        echo "entrypoint: telegram-bot-api started (pid=$TG_PID), waiting for :8081..."

        # Wait up to 30s for telegram-bot-api to bind port 8081
        for i in $(seq 1 30); do
            if (echo > /dev/tcp/127.0.0.1/8081) 2>/dev/null; then
                echo "entrypoint: telegram-bot-api ready"
                break
            fi
            if ! kill -0 "$TG_PID" 2>/dev/null; then
                echo "entrypoint: telegram-bot-api process died (pid=$TG_PID)"
                break
            fi
            sleep 1
        done
    fi
fi

exec server
