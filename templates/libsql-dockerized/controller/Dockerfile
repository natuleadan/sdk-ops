FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /build
# The controller is a standalone module — its go.mod lives inline so the
# template dir stays embed-friendly (go:embed excludes nested modules). The
# source is shipped as main.go.txt so `go mod tidy` in the sdk-ops repo never
# sees it as a package; the Dockerfile renames it back before compiling.
COPY controller/ ./controller/
RUN mv controller/main.go.txt controller/main.go \
 && cd controller && go mod init libsql-controller && go mod tidy && go mod download \
 && CGO_ENABLED=0 GOOS=linux go build -o /controller .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates curl docker-cli
COPY --from=builder /controller /controller
EXPOSE 9090
ENTRYPOINT ["/controller"]
