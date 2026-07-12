FROM docker.dragonflydb.io/dragonflydb/dragonfly AS upstream

FROM alpine:3.21
RUN apk add --no-cache libstdc++ libgcc openssl
COPY --from=upstream /usr/bin/dragonfly /usr/bin/dragonfly
EXPOSE 6379 6380
ENTRYPOINT ["/usr/bin/dragonfly"]
