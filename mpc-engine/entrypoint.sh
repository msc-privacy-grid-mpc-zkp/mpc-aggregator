#!/bin/bash

set -e

MY_ID=$NODE_ID
OUTPUT_FILE="/tmp/Output-P${MY_ID}-0"

case $MY_ID in
    0) GO_HOST="server-a" ;;
    1) GO_HOST="server-b" ;;
    2) GO_HOST="server-c" ;;
esac

echo "======================================================"
echo "🛡️ MPC Worker Node $MY_ID ONLINE"
echo "📡 Target: http://$GO_HOST:8080/api/results"
echo "======================================================"

python3 tcp_receiver.py &
sleep 2

while true; do
    echo "🚀 [$(date +%T)] Running MPC engine..."

    ./malicious-rep-ring-party.x "$MY_ID" variance -v \
        -h mpc-node-a -h mpc-node-b -h mpc-node-c \
        -pn 5000 \
        -IF /tmp/Input \
        -OF /tmp/Output

    echo "📦 MPC finished, waiting for output file..."

    # čekanje da fajl stvarno postoji i bude napisan
    for i in {1..10}; do
        if [ -f "$OUTPUT_FILE" ] && [ -s "$OUTPUT_FILE" ]; then
            break
        fi
        sleep 0.2
    done

    if [ -f "$OUTPUT_FILE" ]; then

        echo "----- RAW OUTPUT BEGIN -----"
        cat "$OUTPUT_FILE"
        echo "----- RAW OUTPUT END -----"

        MEAN=$(grep "RESULT_MEAN:" "$OUTPUT_FILE" | awk -F': ' '{print $2}' | tr -cd '0-9.-')
        VARIANCE=$(grep "RESULT_VARIANCE:" "$OUTPUT_FILE" | awk -F': ' '{print $2}' | tr -cd '0-9.-')

        if [[ -n "$MEAN" && -n "$VARIANCE" && "$MEAN" != *"nan"* ]]; then
            echo "📊 Parsed: Mean=$MEAN Var=$VARIANCE"

            curl -s -X POST "http://$GO_HOST:8080/api/results" \
                -H "Content-Type: application/json" \
                -d "{\"node_id\": $MY_ID, \"mean\": $MEAN, \"variance\": $VARIANCE}"

            echo "✅ Sent to backend"
        else
            echo "⚠️ Parse failed"
        fi
    else
        echo "❌ Output file missing"
    fi

    rm -f "/tmp/Output-P${MY_ID}-0"

    echo "⏳ Restart..."
    sleep 1
done