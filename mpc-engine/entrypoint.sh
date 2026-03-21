#!/bin/bash

MY_ID=$NODE_ID
INPUT_FILE="/tmp/Input-P${MY_ID}-0"

echo "======================================================"
echo "🛡️  MPC Worker Node $MY_ID is ONLINE"
echo "📂 Monitoring: $INPUT_FILE"
echo "======================================================"

mkdir -p Player-Data

# ✅ pokreni TCP server u background
python3 tcp_receiver.py &

# malo sačekaj da se digne server
sleep 1

while true; do
    if [ -f "$INPUT_FILE" ]; then
        echo "🚀 [$(date +%T)] Data detected! Starting MPC..."

        ./malicious-rep-ring-party.x "$MY_ID" variance -v \
            -h mpc-node-a -h mpc-node-b -h mpc-node-c \
            -pn 5000 \
            -IF /tmp/Input \
            -OF /tmp/Output \
            2>&1

        rm "$INPUT_FILE"
        echo "✅ Done. Waiting..."
    fi

    sleep 1
done