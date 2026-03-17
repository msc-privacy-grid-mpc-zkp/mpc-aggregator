#!/bin/bash

# Preuzimamo NODE_ID (0, 1 ili 2) prosleđen kroz docker-compose
MY_ID=$NODE_ID
# Putanja do fajla u RAM disku (Shared Memory) koji pravi Go server
INPUT_FILE="/dev/shm/mp-spdz/Input-P${MY_ID}-0"

echo "======================================================"
echo "🛡️  MPC Worker Node $MY_ID is ONLINE"
echo "📂 Monitoring shared memory: $INPUT_FILE"
echo "======================================================"

# Kreiramo neophodan folder za MP-SPDZ privremene podatke
mkdir -p Player-Data

# Beskonačna petlja koja prati promenu u RAM disku
while true; do
    # Proveravamo da li je Go Aggregator generisao fajl sa udelima
    if [ -f "$INPUT_FILE" ]; then
            echo "🚀 [$(date +%T)] Data detected for Node $MY_ID! Starting MPC..."

            # DIREKTNI POZIV BINARNOG FAJLA
            # Redosled: <izvršni> <player_id> <ime_programa> [opcije]
            ./replicated-ring-party.x "$MY_ID" variance -v \
                -h mpc-node-a -h mpc-node-b -h mpc-node-c \
                -pn 5000 \
                -IF /dev/shm/mp-spdz/Input \
                -OF /dev/shm/mp-spdz/Output \
                2>&1

            rm "$INPUT_FILE"
            echo "✅ [$(date +%T)] Calculation finished. Waiting for next batch..."
    fi
    sleep 1
done