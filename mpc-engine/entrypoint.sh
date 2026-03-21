#!/bin/bash

MY_ID=$NODE_ID
INPUT_FILE="/tmp/Input-P${MY_ID}-0"
OUTPUT_FILE="/tmp/Output-P${MY_ID}-0" # <--- Dodali smo putanju do izlaznog fajla

# Određujemo kom Go serveru ovaj MPC čvor pripada
case $MY_ID in
    0) GO_HOST="server-a" ;;
    1) GO_HOST="server-b" ;;
    2) GO_HOST="server-c" ;;
esac

echo "======================================================"
echo "🛡️  MPC Worker Node $MY_ID is ONLINE (FIFO RAM Mode)"
echo "📂 Listening on Pipe: $INPUT_FILE"
echo "📡 Will report results to: http://$GO_HOST:8080/api/results"
echo "======================================================"

mkdir -p Player-Data

# 1. Pokrećemo Python TCP receiver u pozadini
python3 tcp_receiver.py &

# Čekamo malo da Python stigne da kreira FIFO cev
sleep 2

# 2. Beskonačna petlja za MPC proces
while true; do
    echo "🚀 [$(date +%T)] Waiting for Go Aggregator data..."

    # Pokrećemo MPC. Standardni izlaz hvata bash, ali naši
    # rezultati (print_ln) odlaze u OUTPUT_FILE
    MPC_METRICS=$(./malicious-rep-ring-party.x "$MY_ID" variance -v \
        -h mpc-node-a -h mpc-node-b -h mpc-node-c \
        -pn 5000 \
        -IF /tmp/Input \
        -OF /tmp/Output \
        2>&1)

    # Ispisujemo metriku u terminal
    echo "$MPC_METRICS"

    # 3. PARSIRANJE REZULTATA DIREKTNO IZ FAJLA
    if [ -f "$OUTPUT_FILE" ]; then
        MEAN=$(grep "Mean value (scaled):" "$OUTPUT_FILE" | awk '{print $NF}')
        VARIANCE=$(grep "Variance value (scaled):" "$OUTPUT_FILE" | awk '{print $NF}')

        # AKO SMO USPEŠNO IZVUKLI BROJEVE, ŠALJEMO IH U GO
        if [ ! -z "$MEAN" ] && [ ! -z "$VARIANCE" ]; then
            echo "📤 Sending results to Go Server ($GO_HOST)..."

            curl -s -X POST http://$GO_HOST:8080/api/results \
                 -H "Content-Type: application/json" \
                 -d "{\"node_id\": $MY_ID, \"mean\": $MEAN, \"variance\": $VARIANCE}" > /dev/null

            echo "✅ Results sent successfully!"
        else
            echo "⚠️ Could not parse Mean/Variance from file. Skipping webhook."
        fi
    else
        echo "⚠️ Output file missing! Skipping webhook."
    fi

    echo "⏳ Restarting engine for the next batch..."
    sleep 1
done