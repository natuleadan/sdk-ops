FROM ghcr.io/tursodatabase/libsql-server:v0.24.33

RUN apt-get update -qq && apt-get install -y -qq curl && rm -rf /var/lib/apt/lists/*

COPY snapshot.sh /snapshot.sh
COPY *.lua /tmp/
RUN chmod +x /snapshot.sh
