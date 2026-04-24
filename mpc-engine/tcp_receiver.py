import socket
import os
import stat

NODE_ID = os.environ.get("NODE_ID", "0")
FIFO_PATH = f"/tmp/Input-P{NODE_ID}-0"
HOST = "0.0.0.0"
PORT = 9000

print(f"[TCP] Starting streaming server on port {PORT}...", flush=True)

# 1. Kreiranje FIFO cevi umesto običnog fajla
if os.path.exists(FIFO_PATH):
    # Ako fajl postoji, proveravamo da li je već FIFO
    if not stat.S_ISFIFO(os.stat(FIFO_PATH).st_mode):
        os.remove(FIFO_PATH)
        os.mkfifo(FIFO_PATH)
else:
    os.mkfifo(FIFO_PATH)

print(f"[TCP] FIFO Pipe created at {FIFO_PATH}. Ready for RAM streaming.", flush=True)

# 2. Pokretanje TCP servera
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
# Omogućava brzo ponovno korišćenje porta ako se kontejner restartuje
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((HOST, PORT))
s.listen(5)

while True:
    conn, addr = s.accept()
    print(f"[TCP] Connection established from {addr}", flush=True)

    full_payload = "" # Varijabla u kojoj skupljamo sve što stigne

    # Otvaranje FIFO cevi. Operativni sistem će ovde BLOKIRATI Python skriptu
    # sve dok se MP-SPDZ proces ne probudi sa druge strane da čita podatke!
    with open(FIFO_PATH, "w") as fifo:
        while True:
            data = conn.recv(1024)
            if not data:
                break

            decoded_data = data.decode()
            full_payload += decoded_data # Dodajemo u naš buffer za debug

            # Upisivanje u RAM cev
            fifo.write(decoded_data)
            fifo.flush() # Kritično: forsira guranje podataka kroz cev

    # --- 🔍 POČETAK DEBUG LOGA ---
    # Sada kada imamo ceo paket, hajde da ga analiziramo
    lines = [x.strip() for x in full_payload.split('\n') if x.strip()]
    if len(lines) > 0:
        try:
            n_share = int(lines[0]) # Prva linija je uvek share broja 'N'

            # Sve ostale linije su shares potrošnje (i nule od Zero-Paddinga)
            # Nule ne utiču na zbir, pa možemo samo da saberemo ceo ostatak liste
            meter_shares = [int(x) for x in lines[1:]]
            sum_of_shares = sum(meter_shares)

            print(f"\n🐍 [DEBUG PYTHON Node {NODE_ID}] --- ZAVRŠEN PRIJEM ---", flush=True)
            print(f"   -> Ukupno primljeno linija: {len(lines)}", flush=True)
            print(f"   -> Udeo broja merača (N-Share): {n_share}", flush=True)
            print(f"   -> ZBIR UDELA POTROŠNJE NA OVOM ČVORU: {sum_of_shares}", flush=True)
            print(f"--------------------------------------------------\n", flush=True)
        except Exception as e:
            print(f"🐍 [DEBUG PYTHON Node {NODE_ID}] Greška pri parsiranju debug loga: {e}", flush=True)
    # --- 🔍 KRAJ DEBUG LOGA ---

    conn.close()
    print("[TCP] Data batch streamed to MPC successfully.", flush=True)