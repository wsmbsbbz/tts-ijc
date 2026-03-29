#!/bin/sh
set -e

# Start telegram-bot-api in local mode if credentials are provided.
# Running in the same container gives the main app direct filesystem access
# to downloaded files, bypassing the broken HTTP file-serving endpoint.
if [ -n "$TELEGRAM_API_ID" ] && [ -n "$TELEGRAM_API_HASH" ]; then
    mkdir -p /var/lib/telegram-bot-api
    telegram-bot-api \
        --api-id="$TELEGRAM_API_ID" \
        --api-hash="$TELEGRAM_API_HASH" \
        --local \
        --dir=/var/lib/telegram-bot-api \
        --port=8081 \
        --no-interactive &
    echo "telegram-bot-api: started on :8081"
fi

exec server
