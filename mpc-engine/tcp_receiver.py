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

NODE_ID = os.environ.get("NODE_ID", "0")
HOST = "0.0.0.0"
PORT = 9000

GO_HOSTS = {"0": "server-a", "1": "server-b", "2": "server-c"}
GO_HOST = GO_HOSTS.get(NODE_ID, "server-a")
GO_URL_HTTP = f"http://{GO_HOST}:8080/api/results"
MTLS_PORT = os.environ.get("MTLS_PORT", "8443")
GO_URL_MTLS = f"https://{GO_HOST}:{MTLS_PORT}/api/results"  # mTLS listener (configurable)
# Default to HTTP for external callers. When a client cert is available we'll use the mTLS URL.
GO_URL = GO_URL_HTTP

# Configure Python logging with per-node context
try:
    logging.basicConfig(level=logging.INFO, format=f'%(asctime)s %(levelname)s [node:{NODE_ID}] %(message)s')
except Exception:
    # Fallback to a simpler format if the formatter fails for any reason
    logging.basicConfig(level=logging.INFO, format='%(asctime)s %(levelname)s %(message)s')
logger = logging.getLogger("tcp_receiver")

# ====================================================================
# HELPER: Clean invalid values (NaN, Inf, None)
# ====================================================================
def clean_val(val):
    """
    Sanitize a value by checking for None, NaN, and Inf.
    Returns 0.0 if any of these conditions are true, otherwise returns the value.
    """
    if val is None:
        logger.warning("[SECURITY] Received None value; replacing with 0.0")
        return 0.0
    try:
        float_val = float(val)
        if math.isnan(float_val):
            logger.warning("[SECURITY] Received NaN value; replacing with 0.0")
            return 0.0
        if math.isinf(float_val):
            logger.warning("[SECURITY] Received Inf value; replacing with 0.0")
            return 0.0
        return float_val
    except (ValueError, TypeError) as e:
        logger.warning(f"[SECURITY] Failed to convert value to float: {e}; replacing with 0.0")
        return 0.0

logger.info(f"[TCP] Starting direct-stream server on port {PORT}")

s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((HOST, PORT))
s.listen(5)

logger.info("[TCP] Server listening. Using direct subprocess STDIN.")

while True:
    conn, addr = s.accept()
    logger.info(f"[TCP] Connection established from {addr}")

    full_payload = ""
    while True:
        try:
            data = conn.recv(1024)
        except Exception as e:
            logger.error(f"[ERROR] Socket recv failed: {e}")
            data = b""
        if not data:
            break
        full_payload += data.decode()

    conn.close()

    lines = [x.strip() for x in full_payload.split('\n') if x.strip()]
    if not lines:
        continue

    logger.debug(f"[DEBUG] Received {len(lines)} lines. Launching MP-SPDZ engine...")

    mpc_cmd = [
        "./malicious-rep-ring-party.x", str(NODE_ID), "aggregate_stats", "-v",
        "-h", "mpc-node-a", "-h", "mpc-node-b", "-h", "mpc-node-c",
        "-pn", "5000",
        "-I"
    ]

    process = subprocess.Popen(
        mpc_cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True
    )

    # Streaming stdin/stdout to avoid deadlocks and unbounded memory usage
    max_output = int(os.environ.get("MP_SPZD_OUTPUT_MAX", 1 * 1024 * 1024))
    spdz_timeout = int(os.environ.get("MP_SPZD_TIMEOUT", 60))

    stdout_buf = io.StringIO()

    def _reader():
        try:
            # read line-by-line until EOF
            for line in iter(process.stdout.readline, ''):
                if not line:
                    break
                stdout_buf.write(line)
                if stdout_buf.tell() > max_output:
                    logger.error(f"[ERROR] MP-SPDZ output exceeded {max_output} bytes; terminating process")
                    try:
                        process.kill()
                    except Exception:
                        pass
                    break
        except Exception as e:
            logger.error(f"[ERROR] stdout reader exception: {e}")

    def _writer():
        try:
            if process.stdin:
                process.stdin.write(full_payload)
                process.stdin.flush()
                process.stdin.close()
        except Exception as e:
            logger.error(f"[ERROR] stdin writer exception: {e}")

    reader_t = threading.Thread(target=_reader, daemon=True)
    writer_t = threading.Thread(target=_writer, daemon=True)
    reader_t.start()
    writer_t.start()

    try:
        process.wait(timeout=spdz_timeout)
    except subprocess.TimeoutExpired:
        try:
            process.kill()
        except Exception:
            pass
        logger.error("[ERROR] MP-SPDZ timed out and was killed")

    # give threads a moment to finish reading
    reader_t.join(timeout=1)
    writer_t.join(timeout=1)

    stdout_data = stdout_buf.getvalue()

    # Cap stdout size to prevent memory exhaustion
    if len(stdout_data) > max_output:
        logger.error(f"[ERROR] MP-SPDZ output too large: {len(stdout_data)} bytes")
        continue

    logger.debug(stdout_data)

    mean, total_power, variance = None, None, None
    for line in stdout_data.split('\n'):
        if "RESULT_MEAN:" in line:
            mean = line.split(":")[1].strip()
        if "RESULT_TOTAL_POWER:" in line:
            total_power = line.split(":")[1].strip()
        if "RESULT_VARIANCE:" in line:
            variance = line.split(":")[1].strip()

    if mean and total_power:
        # ====================================================================
        # SANITIZE VALUES: Remove NaN, Inf, None
        # ====================================================================
        total_power_clean = clean_val(total_power)
        mean_clean = clean_val(mean)
        variance_clean = clean_val(variance) if variance else 0.0

        # ====================================================================
        # DATA POISONING DETECTION: Check if mean is within physical limits
        # ====================================================================
        if mean_clean < 0.0 or mean_clean > 10000.0:
            logger.warning(f"[SECURITY WARNING] Data Poisoning detected: mean={mean_clean}W is outside valid range [0, 10000]W")

        logger.info(f"[INFO] Results Parsed: Total={total_power_clean}W, Mean={mean_clean}W, Var={variance_clean}")

        payload = {
            "node_id": int(NODE_ID),
            "total_power": total_power_clean,
            "mean": mean_clean,
            "variance": variance_clean
        }

        payload_bytes = json.dumps(payload).encode('utf-8')

        # Compute HMAC if secret provided
        SHARED_SECRET = os.environ.get("RESULTS_SHARED_SECRET", "")
        headers = {'Content-Type': 'application/json'}
        if SHARED_SECRET:
            import hmac, hashlib
            sig = hmac.new(SHARED_SECRET.encode(), payload_bytes, hashlib.sha256).hexdigest()
            headers['X-Signature'] = sig

        # Use mTLS if client cert and CA are available (checked once)
        def resolve_secret(p):
            # If path exists and is file -> return
            if os.path.exists(p) and not os.path.isdir(p):
                return p
            # If path is dir -> pick first regular file inside
            if os.path.isdir(p):
                for entry in os.listdir(p):
                    candidate = os.path.join(p, entry)
                    if os.path.isfile(candidate):
                        return candidate
            # try with .pem suffix
            if os.path.exists(p + '.pem'):
                return p + '.pem'
            return None

        # Prefer node-specific certs (recommended if secrets directory contains per-node files).
        client_cert = resolve_secret(f'/run/secrets/tls_client_cert_node{NODE_ID}.pem')
        client_key = resolve_secret(f'/run/secrets/tls_client_key_node{NODE_ID}.pem')
        # Fallback to generic names if node-specific files not present.
        if not client_cert:
            client_cert = resolve_secret('/run/secrets/tls_client_cert')
        if not client_key:
            client_key = resolve_secret('/run/secrets/tls_client_key')
        ca_cert = resolve_secret('/run/secrets/tls_ca_cert')

        # DEBUG: log resolved cert paths and CN (if openssl available)
        cn_info = "<not-available>"
        if client_cert:
            try:
                cn_out = subprocess.check_output(["openssl", "x509", "-in", client_cert, "-noout", "-subject"], stderr=subprocess.STDOUT).decode().strip()
                cn_info = cn_out
            except Exception as e:
                cn_info = f"<err:{e}>"
        logger.debug(f"[DEBUG] Resolved client_cert={client_cert}, client_key={client_key}, ca_cert={ca_cert}, cert_subject={cn_info}")

        # Retry with exponential backoff
        attempts = 3
        for attempt in range(1, attempts + 1):
            try:
                if client_cert and client_key:
                    # requests with mTLS (use mTLS endpoint)
                    import requests
                    cert = (client_cert, client_key)
                    verify = ca_cert if ca_cert else True
                    target_url = GO_URL_MTLS

                    # DEBUG: log verbose info about the mTLS request to help troubleshoot 403/401
                    try:
                        cert_subject = cn_info
                    except NameError:
                        cert_subject = "<unknown>"
                    logger.debug(f"[DEBUG] Preparing mTLS POST to {target_url}; node_id={payload.get('node_id')} cert={client_cert} key={client_key} ca={ca_cert} cert_subject={cert_subject}")
                    logger.debug(f"[DEBUG] Payload preview: {json.dumps(payload)[:200]}")

                    try:
                        resp = requests.post(target_url, data=payload_bytes, headers=headers, cert=cert, verify=verify, timeout=5)
                        if resp.status_code == 200:
                            logger.info(f"[INFO] Results sent successfully to {target_url}")
                            break
                        else:
                            body = resp.text if hasattr(resp, 'text') else ''
                            truncated = (body[:1024] + '...') if len(body) > 1024 else body
                            logger.warning(f"[WARNING] Unexpected response {resp.status_code} from server {target_url}; body: {truncated}")
                    except Exception as e:
                        logger.error(f"[ERROR] mTLS POST to {target_url} failed: {e}")
                else:
                    # fallback to urllib (HTTP)
                    target_url = GO_URL_HTTP
                    req = urllib.request.Request(target_url, data=payload_bytes, headers=headers)

                    # DEBUG: log HTTP fallback info
                    logger.debug(f"[DEBUG] Preparing HTTP POST to {target_url}; node_id={payload.get('node_id')}")

                    try:
                        with urllib.request.urlopen(req, timeout=5) as resp:
                            if resp.status == 200:
                                logger.info(f"[INFO] Results sent successfully to {target_url}")
                                break
                            else:
                                body = resp.read().decode(errors='replace')
                                truncated = (body[:1024] + '...') if len(body) > 1024 else body
                                logger.warning(f"[WARNING] Unexpected response {resp.status} from server {target_url}; body: {truncated}")
                    except urllib.error.HTTPError as e:
                        try:
                            body = e.read().decode(errors='replace')
                        except Exception:
                            body = ''
                        truncated = (body[:1024] + '...') if len(body) > 1024 else body
                        logger.warning(f"[WARNING] HTTPError from {target_url}: {e.code}; body: {truncated}")
                    except Exception as e:
                        logger.error(f"[ERROR] Error posting to {target_url}: {e}")
            except Exception as e:
                logger.error(f"[ERROR] Error sending results to Go (attempt {attempt}): {e}")
                if attempt < attempts:
                    time.sleep(2 ** attempt)
                else:
                    logger.error(f"[ERROR] Giving up sending results after {attempts} attempts")
