import socket
import os
import stat

NODE_ID = os.environ.get("NODE_ID", "0")
FIFO_PATH = f"/tmp/Input-P{NODE_ID}-0"
HOST = "0.0.0.0"
PORT = 9000

print(f"[TCP] Starting streaming server on port {PORT}...")

# 1. Kreiranje FIFO cevi umesto običnog fajla
if os.path.exists(FIFO_PATH):
    # Ako fajl postoji, proveravamo da li je već FIFO
    if not stat.S_ISFIFO(os.stat(FIFO_PATH).st_mode):
        os.remove(FIFO_PATH)
        os.mkfifo(FIFO_PATH)
else:
    os.mkfifo(FIFO_PATH)

print(f"[TCP] FIFO Pipe created at {FIFO_PATH}. Ready for RAM streaming.")

# 2. Pokretanje TCP servera
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind((HOST, PORT))
s.listen(5)

while True:
    conn, addr = s.accept()
    print(f"[TCP] Connection established from {addr}")

    # Otvaranje FIFO cevi.
    # Napomena: Operativni sistem će ovde BLOKIRATI Python skriptu
    # sve dok se MP-SPDZ proces ne probudi sa druge strane da čita podatke!
    with open(FIFO_PATH, "w") as fifo:
        while True:
            data = conn.recv(1024)
            if not data:
                break

            # Upisivanje u RAM cev
            fifo.write(data.decode())
            fifo.flush() # Kritično: forsira guranje podataka kroz cev

    conn.close()
    print("[TCP] Data batch streamed to MPC successfully.")