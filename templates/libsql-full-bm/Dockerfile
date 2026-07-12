FROM ghcr.io/tursodatabase/libsql-server:latest

RUN apt-get update -qq && apt-get install -y -qq curl && rm -rf /var/lib/apt/lists/*

COPY snapshot.sh /snapshot.sh
RUN chmod +x /snapshot.sh
