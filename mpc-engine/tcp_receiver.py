import socket
import os
import subprocess
import urllib.request
import json
import time
import logging
import threading
import io
import math
import requests
import ssl
from requests.adapters import HTTPAdapter
from urllib3.poolmanager import PoolManager

# --- Konfiguracija ---
NODE_ID = os.environ.get("NODE_ID", "0")
HOST = "0.0.0.0"
PORT = 9000

# Docker networking
GO_HOSTS = {"0": "server-a", "1": "server-b", "2": "server-c"}
GO_HOST = GO_HOSTS.get(NODE_ID, "server-a")

# URL-ovi
GO_URL_HTTP = f"http://{GO_HOST}:8080/api/results"
# OBAVEZNO: Ovde stoji 8443 port za mTLS
MTLS_PORT = "8443"
GO_URL_MTLS = f"https://{GO_HOST}:{MTLS_PORT}/api/results"

# Konfiguracija logovanja
try:
    logging.basicConfig(level=logging.INFO, format=f'%(asctime)s %(levelname)s [node:{NODE_ID}] %(message)s')
except Exception:
    logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname)s %(message)s')
logger = logging.getLogger("tcp_receiver")

# Adapter koji dozvoljava mTLS vezu bez provere hostname-a (zaobilazi mismatch)
class NoVerifyHostnameAdapter(HTTPAdapter):
    def __init__(self, ca_cert):
        self.ca_cert = ca_cert
        super().__init__()
    def init_poolmanager(self, connections, maxsize, block=False):
        ctx = ssl.create_default_context(cafile=self.ca_cert)
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_REQUIRED
        self.poolmanager = PoolManager(ssl_context=ctx)
        return self.poolmanager

def clean_val(val):
    if val is None: return 0.0
    try:
        f = float(val)
        return 0.0 if (math.isnan(f) or math.isinf(f)) else f
    except: return 0.0

# --- Start TCP Server ---
logger.info(f"[TCP] Starting direct-stream server on port {PORT}")
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((HOST, PORT))
s.listen(5)

while True:
    conn, addr = s.accept()
    full_payload = ""
    while True:
        data = conn.recv(1024)
        if not data: break
        full_payload += data.decode()
    conn.close()

    lines = [x.strip() for x in full_payload.split('\n') if x.strip()]
    if not lines: continue

    mpc_cmd = ["./malicious-rep-ring-party.x", str(NODE_ID), "aggregate_stats", "-v", "-h", "mpc-node-a", "-h", "mpc-node-b", "-h", "mpc-node-c", "-pn", "5000", "-I"]
    process = subprocess.Popen(mpc_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)

    stdout_buf = io.StringIO()
    def _reader():
        for line in iter(process.stdout.readline, ''): stdout_buf.write(line)

    reader_t = threading.Thread(target=_reader, daemon=True)
    reader_t.start()

    if process.stdin:
        process.stdin.write(full_payload)
        process.stdin.flush()
        process.stdin.close()

    process.wait(timeout=60)
    reader_t.join(timeout=1)
    stdout_data = stdout_buf.getvalue()

    # Parsiranje rezultata
    mean, total_power, variance, poisoned_count = None, None, None, None
    for line in stdout_data.split('\n'):
        if "RESULT_MEAN:" in line: mean = line.split(":")[1].strip()
        if "RESULT_TOTAL_POWER:" in line: total_power = line.split(":")[1].strip()
        if "RESULT_VARIANCE:" in line: variance = line.split(":")[1].strip()
        if "RESULT_POISONED_COUNT:" in line: poisoned_count = line.split(":")[1].strip()

    if mean and total_power:
        payload = {"node_id": int(NODE_ID), "total_power": clean_val(total_power), "mean": clean_val(mean), "variance": clean_val(variance)}
        payload_bytes = json.dumps(payload).encode('utf-8')

        def resolve_secret(p):
            if os.path.exists(p) and not os.path.isdir(p): return p
            if os.path.exists(p + '.pem'): return p + '.pem'
            return None

        c_cert = resolve_secret(f'/run/secrets/tls_client_cert_node{NODE_ID}')
        c_key = resolve_secret(f'/run/secrets/tls_client_key_node{NODE_ID}')
        c_ca = resolve_secret('/run/secrets/tls_ca_cert')

        for attempt in range(1, 4):
            try:
                if c_cert and c_key and c_ca:
                    session = requests.Session()
                    session.mount('https://', NoVerifyHostnameAdapter(c_ca))
                    resp = session.post(GO_URL_MTLS, data=payload_bytes, cert=(c_cert, c_key), timeout=5)
                    if resp.status_code == 200:
                        logger.info(f"[SUCCESS] Rezultati poslati preko mTLS-a na {GO_URL_MTLS}")
                        break
                    else:
                        logger.warning(f"[WARNING] Server odgovorio: {resp.status_code}")
                else:
                    raise Exception("Sertifikati nisu pronađeni")
            except Exception as e:
                logger.error(f"[ERROR] Pokušaj {attempt} nije uspeo: {e}")
                time.sleep(2**attempt)