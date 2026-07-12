FROM postgres:18-alpine

RUN apk add --no-cache pgbackrest

RUN mkdir -p -m 750 /var/lib/pgbackrest && chown postgres:postgres /var/lib/pgbackrest && \
    mkdir -p -m 770 /var/log/pgbackrest && chown postgres:postgres /var/log/pgbackrest && \
    mkdir -p -m 777 /tmp/pgbackrest && \
    mkdir -p -m 775 /var/spool/pgbackrest && chown postgres:postgres /var/spool/pgbackrest && \
    mkdir -p /etc/pgbackrest

COPY pg-entrypoint.sh /usr/local/bin/pg-entrypoint.sh
COPY pgbackrest.conf /etc/pgbackrest/pgbackrest.conf

RUN chmod 644 /etc/pgbackrest/pgbackrest.conf && chown postgres:postgres /etc/pgbackrest/pgbackrest.conf

ENTRYPOINT ["/usr/local/bin/pg-entrypoint.sh"]
CMD ["postgres"]
