# df-dockerized — pinned DragonflyDB build (ARG overrides at compose build).
# The version lives in the compose `build.args` (single source of truth).
ARG DF_TAG=v1.30.1
FROM docker.dragonflydb.io/dragonflydb/dragonfly:${DF_TAG} AS upstream

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends libz1 redis-tools && rm -rf /var/lib/apt/lists/*
COPY --from=upstream /usr/local/bin/dragonfly /usr/local/bin/dragonfly
EXPOSE 6379 6380
ENTRYPOINT ["/usr/local/bin/dragonfly"]
