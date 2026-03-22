FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# 1. Instalacija sistemskih zavisnosti uz minimalne pakete
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-dev python3-pip \
    git build-essential clang lld \
    libgmp3-dev libntl-dev libsodium-dev \
    libboost-dev libboost-thread-dev libboost-system-dev libboost-iostreams-dev libboost-filesystem-dev \
    libssl-dev \
    automake libtool tox \
    iputils-ping netcat-openbsd curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 2. Plitko kloniranje repozitorijuma za brži build i manji image
RUN git clone --depth 1 https://github.com/data61/MP-SPDZ.git /mp-spdz
WORKDIR /mp-spdz

# 3. GLOBALNI ARGUMENT
ARG NUM_PARTIES=3

# 4. Generisanje SSL sertifikata
RUN Scripts/setup-ssl.sh $NUM_PARTIES

# 5. Konfiguracija kompajlera za maksimalne performanse
# Dodajemo -march=native za hardversku akceleraciju i -O3/-flto za optimizaciju
RUN echo "CXX = clang++" > CONFIG.mine && \
    echo "ARCH = -march=native" >> CONFIG.mine && \
    echo "MY_CFLAGS = -I/usr/local/include -DINSECURE -O3 -flto" >> CONFIG.mine && \
    echo "MY_LDLIBS = -L/usr/local/lib" >> CONFIG.mine && \
    echo "USE_NTL = 1" >> CONFIG.mine

# 6. Kompajliranje specifičnog protokola koristeći sva dostupna jezgra
RUN make -j$(nproc) malicious-rep-ring-party.x

# 7. Kopiranje tvog koda, skripti i kompilacija kola
COPY ./src/variance.mpc ./Programs/Source/
COPY ./entrypoint.sh ./entrypoint.sh
COPY ./tcp_receiver.py ./tcp_receiver.py

# Spajamo chmod, sed i compile komande u jedan RUN sloj radi optimizacije imidža
RUN chmod +x ./entrypoint.sh && \
    sed -i "s/@@NUM_PARTIES@@/$NUM_PARTIES/g" ./Programs/Source/variance.mpc && \
    # Dodata flag optimizacija u compile.py ako kolo to dozvoljava
    ./compile.py -R 128 variance

ENTRYPOINT ["./entrypoint.sh"]