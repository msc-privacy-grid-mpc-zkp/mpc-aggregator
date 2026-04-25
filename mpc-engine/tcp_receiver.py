import socket
import os
import subprocess
import urllib.request
import json

NODE_ID = os.environ.get("NODE_ID", "0")
HOST = "0.0.0.0"
PORT = 9000

# Mapiranje na Go servere
GO_HOSTS = {"0": "server-a", "1": "server-b", "2": "server-c"}
GO_HOST = GO_HOSTS.get(NODE_ID, "server-a")
GO_URL = f"http://{GO_HOST}:8080/api/results"

print(f"[TCP] Starting direct-stream server on port {PORT}...", flush=True)

# Pokretanje TCP servera
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((HOST, PORT))
s.listen(5)

print(f"[TCP] Server listening. FIFO is REMOVED. Using direct subprocess STDIN.", flush=True)

while True:
    conn, addr = s.accept()
    print(f"\n[TCP] Connection established from {addr}", flush=True)

    full_payload = ""
    while True:
        data = conn.recv(1024)
        if not data:
            break
        full_payload += data.decode()

    conn.close()

    lines = [x.strip() for x in full_payload.split('\n') if x.strip()]
    if not lines:
        continue

    print(f"🐍 [DEBUG PYTHON] Primljeno {len(lines)} linija. Pokrećem MP-SPDZ Engine...", flush=True)

    # Pokrećemo MP-SPDZ direktno iz Pythona!
    # Fleg -I je KRITIČAN: Znači da MP-SPDZ čita direktno iz Python STDIN-a
    mpc_cmd = [
        "./malicious-rep-ring-party.x", str(NODE_ID), "variance", "-v",
        "-h", "mpc-node-a", "-h", "mpc-node-b", "-h", "mpc-node-c",
        "-pn", "5000",
        "-I"
    ]

    # Povezujemo se na proces
    process = subprocess.Popen(
        mpc_cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, # Hvatamo i greške i logove
        text=True
    )

    # GURAMO PODATKE DIREKTNO U PROCES (RAM-to-RAM prenos) i čekamo rezultat
    stdout_data, _ = process.communicate(input=full_payload)

    # Štampamo ceo MP-SPDZ izlaz da bismo imali logove u Dockeru
    print(stdout_data, flush=True)

    # PARSIRANJE REZULTATA DIREKTNO IZ STDOUT-a
    mean, total_power, variance = None, None, None
    for line in stdout_data.split('\n'):
        if "RESULT_MEAN:" in line:
            mean = line.split(":")[1].strip()
        if "RESULT_TOTAL_POWER:" in line:
            total_power = line.split(":")[1].strip()
        if "RESULT_VARIANCE:" in line:
            variance = line.split(":")[1].strip()

    # SLANJE NAZAD U GO AGREGATOR
    if mean and total_power:
        print(f"📊 Results Parsed: Total={total_power}W, Mean={mean}W, Var={variance}", flush=True)

        payload = {
            "node_id": int(NODE_ID),
            "total_power": float(total_power),
            "mean": float(mean),
            "variance": float(variance) if variance else 0.0
        }

        try:
            req = urllib.request.Request(GO_URL, data=json.dumps(payload).encode('utf-8'), headers={'Content-Type': 'application/json'})
            urllib.request.urlopen(req)
            print(f"✅ Results sent successfully to {GO_HOST}!", flush=True)
        except Exception as e:
            print(f"⚠️ Error sending results to Go: {e}", flush=True)