# aggregator
EN: This repository contains the backend (Aggregator) for a privacy-preserving smart meter infrastructure. Acting as the central node, it receives serialized Zero-Knowledge Proofs (Groth16) from edge devices, cryptographically verifies that consumption readings are within valid physical limits without revealing the actual values, and securely aggregates the data using Multi-Party Computation (MPC).

SR: Ovaj repozitorijum sadrži backend (Agregator) za infrastrukturu pametnih brojila baziranu na zaštiti privatnosti potrošača. Kao centralni čvor, prima serijalizovane Zero-Knowledge dokaze (Groth16) sa Edge uređaja, kriptografski verifikuje da su očitavanja u granicama fizičkih limita bez otkrivanja stvarnih vrednosti, i bezbedno agregira podatke koristeći Multi-Party Computation (MPC).

## Configuration example

A sample `config.example.yaml` is provided in the repository root. It shows how to configure server ports, ZKP parameters and the client certificate CN→NodeID mapping used for mTLS authorization.

Key fields:
- server.port: HTTP port for external (non-mTLS) clients (default: 8080)
- aggregator.node_id: Node identifier for this server
- client_cn_map: mapping of client certificate CommonName (CN) to expected NodeID; used to authorize mTLS clients for `/api/results`.

Usage:
1. Copy `config.example.yaml` to `config.yaml` and edit values as appropriate.
2. Ensure per-node client certificates are generated and their CNs match keys in `client_cn_map`.

See `config.example.yaml` for a full example.
