FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags 2>/dev/null || echo dev)" -o sdk-ops-agent ./agent/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata docker-cli
RUN adduser -D -h /data agent
COPY --from=builder /build/sdk-ops-agent /usr/local/bin/
USER agent
EXPOSE 9000
VOLUME /data
ENTRYPOINT ["sdk-ops-agent"]
