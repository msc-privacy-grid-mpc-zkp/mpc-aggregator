FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# 1. Instalacija sistemskih zavisnosti
RUN apt-get update && apt-get install -y \
    python3 python3-dev python3-pip \
    git build-essential clang lld \
    libgmp3-dev libntl-dev libsodium-dev \
    libboost-dev libboost-thread-dev libboost-system-dev libboost-iostreams-dev libboost-filesystem-dev \
    libssl-dev \
    automake libtool tox \
    iputils-ping netcat-openbsd curl \
    && rm -rf /var/lib/apt/lists/*

# 2. Kloniranje MP-SPDZ repozitorijuma
RUN git clone https://github.com/data61/MP-SPDZ.git /mp-spdz
WORKDIR /mp-spdz

# 3. GLOBALNI ARGUMENT: Ovde definišeš broj partija (default 3)
ARG NUM_PARTIES=3

# 4. Generisanje SSL sertifikata za tačan broj partija
RUN Scripts/setup-ssl.sh $NUM_PARTIES

# 5. Konfiguracija kompajlera i mašine
RUN echo "CXX = clang++" > CONFIG.mine && \
    echo "MY_CFLAGS = -I/usr/local/include -DINSECURE" >> CONFIG.mine && \
    echo "MY_LDLIBS = -L/usr/local/lib" >> CONFIG.mine

# 6. Kompajliranje specifičnog protokola (malicious-rep-ring)
RUN make -j8 malicious-rep-ring-party.x

# 7. Kopiranje tvog koda i skripti
COPY ./src/variance.mpc ./Programs/Source/
COPY ./entrypoint.sh ./entrypoint.sh
COPY ./tcp_receiver.py ./tcp_receiver.py
RUN chmod +x ./entrypoint.sh

# 8. SED INJECTION: Menjamo marker @@NUM_PARTIES@@ u fajlu pravom vrednošću
# Ovo osigurava da skripta uvek zna tačan broj čvorova
RUN sed -i "s/@@NUM_PARTIES@@/$NUM_PARTIES/g" ./Programs/Source/variance.mpc

# 9. Kompilacija MPC kola (sada bez eksternih args jer je broj upisan u kod)
RUN ./compile.py -R 128 variance

ENTRYPOINT ["./entrypoint.sh"]