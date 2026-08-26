# patroni image — postgres 18 + Patroni + pgbackrest (custom; public images need auth).
# Patroni is PINNED to the stable 4.1.4 (the pip latest at build time drifts).
FROM postgres:18-alpine

RUN apk add --no-cache python3 py3-pip py3-psycopg2 curl pgbackrest \
    && pip3 install --no-cache-dir --break-system-packages 'patroni[etcd]==4.1.4' \
    && mkdir -p /etc/patroni

COPY patroni.yml /etc/patroni/patroni.yml

ENTRYPOINT ["/usr/bin/patroni"]
CMD ["/etc/patroni/patroni.yml"]
