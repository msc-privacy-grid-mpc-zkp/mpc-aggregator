#!/bin/bash

MY_ID=$NODE_ID
OUTPUT_FILE="/tmp/Output-P${MY_ID}-0"

# Mapiranje na Go servere
case $MY_ID in
    0) GO_HOST="server-a" ;;
    1) GO_HOST="server-b" ;;
    2) GO_HOST="server-c" ;;
esac

echo "======================================================"
echo "🛡️  MPC Worker Node $MY_ID is ONLINE (TOTAL PRIVACY)"
echo "📡 Reporting to: http://$GO_HOST:8080/api/results"
echo "======================================================"

python3 tcp_receiver.py &
sleep 2

while true; do
    echo "🚀 [$(date +%T)] Waiting for Go Aggregator data..."

    # Izvršavanje MPC engine-a
    ./malicious-rep-ring-party.x "$MY_ID" variance -v \
        -h mpc-node-a -h mpc-node-b -h mpc-node-c \
        -pn 5000 \
        -IF /tmp/Input \
        -OF /tmp/Output > /dev/null 2>&1

    # 2. PARSIRANJE REZULTATA - Koristimo nove tagove iz .mpc skripte
    if [ -f "$OUTPUT_FILE" ]; then
        # Tražimo RESULT_MEAN: i uzimamo broj nakon toga, čistimo nevidljive karaktere
        MEAN=$(grep "RESULT_MEAN:" "$OUTPUT_FILE" | awk -F': ' '{print $2}' | tr -cd '0-9.-')

        # Tražimo RESULT_VARIANCE: i uzimamo broj nakon toga
        VARIANCE=$(grep "RESULT_VARIANCE:" "$OUTPUT_FILE" | awk -F': ' '{print $2}' | tr -cd '0-9.-')

        # 3. SLANJE REZULTATA
        if [[ ! -z "$MEAN" && "$MEAN" != *"nan"* && ! -z "$VARIANCE" && "$VARIANCE" != *"nan"* ]]; then
            echo "📊 Results Parsed: Mean=$MEAN, Var=$VARIANCE"
            echo "📤 Sending to Go Server ($GO_HOST)..."

            curl -s -X POST "http://$GO_HOST:8080/api/results" \
                 -H "Content-Type: application/json" \
                 -d "{\"node_id\": $MY_ID, \"mean\": $MEAN, \"variance\": $VARIANCE}"

            echo "✅ Results sent successfully!"
        else
            echo "⚠️ Parsing failed. Expected RESULT_MEAN/VARIANCE in $OUTPUT_FILE"
            echo "Sadržaj fajla:"
            cat "$OUTPUT_FILE"
        fi
    else
        echo "⚠️ Output file $OUTPUT_FILE missing!"
    fi

    # Čistimo izlazni fajl za sledeći batch da ne bi grep čitao stare rezultate
    rm -f "$OUTPUT_FILE"

    echo "⏳ Restarting engine..."
    sleep 1
done