import socket
import os

NODE_ID = os.environ.get("NODE_ID", "0")
INPUT_FILE = f"/tmp/Input-P{NODE_ID}-0"

HOST = "0.0.0.0"
PORT = 9000

print(f"[TCP] Starting server on port {PORT}...")

s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind((HOST, PORT))
s.listen(5)

while True:
    conn, addr = s.accept()
    print(f"[TCP] Connection from {addr}")

    with open(INPUT_FILE, "w") as f:
        while True:
            data = conn.recv(1024)
            if not data:
                break
            f.write(data.decode())

    conn.close()
    print("[TCP] File received")