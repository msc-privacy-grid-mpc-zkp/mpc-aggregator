#!/bin/bash

MY_ID=$NODE_ID

echo "======================================================"
echo "🛡️  MPC Worker Node $MY_ID is ONLINE (DIRECT RAM STREAMING)"
echo "======================================================"

# Python skripta sada sluša port 9000 i sama upravlja C++ mašinom
python3 tcp_receiver.py