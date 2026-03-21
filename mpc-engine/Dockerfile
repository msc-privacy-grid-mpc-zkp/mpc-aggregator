FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Instalacija svih potrebnih biblioteka i alata
RUN apt-get update && apt-get install -y \
    python3 python3-dev python3-pip \
    git build-essential clang lld \
    libgmp3-dev libntl-dev libsodium-dev \
    libboost-dev libboost-thread-dev libboost-system-dev libboost-iostreams-dev libboost-filesystem-dev \
    libssl-dev \
    automake libtool tox \
    iputils-ping netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /mp-spdz

RUN git clone https://github.com/data61/MP-SPDZ.git .

# 1. GENERISANJE SERTIFIKATA
# Pravimo sertifikate za 3 igrača (Player 0, 1 i 2)
ARG NUM_PARTIES=3
RUN Scripts/setup-ssl.sh $NUM_PARTIES

# 2. CONFIGURACIJA
# Dodajemo -DINSECURE u MY_CFLAGS da bismo izbegli "benchmarking functionality" grešku
# i omogućili rad u kontejnerima.
RUN echo "CXX = clang++" > CONFIG.mine && \
    echo "MY_CFLAGS = -I/usr/local/include -DINSECURE" >> CONFIG.mine && \
    echo "MY_LDLIBS = -L/usr/local/lib" >> CONFIG.mine

# Kompajliranje mašine za prsten (replicated ring)
RUN make -j8 malicious-rep-ring-party.x

# Kopiranje izvornog koda i skripte
COPY ./src/variance.mpc ./Programs/Source/
COPY ./entrypoint.sh ./entrypoint.sh
COPY ./tcp_receiver.py ./tcp_receiver.py
RUN chmod +x ./entrypoint.sh

RUN echo "Compiling new version..." && date

ARG NUM_METERS=10

RUN echo "Compiling new version with NUM_METERS=$NUM_METERS..." && date

RUN sed -i "s/num_meters = [0-9]*/num_meters = $NUM_METERS/g" ./Programs/Source/variance.mpc

RUN grep "num_meters" ./Programs/Source/variance.mpc

RUN ./compile.py -R 128 variance

ENTRYPOINT ["./entrypoint.sh"]