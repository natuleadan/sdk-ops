FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /build
# The router is a standalone module — its go.mod lives inline so the template
# dir stays embed-friendly (go:embed excludes nested modules). The source is
# shipped as main.go.txt so `go mod tidy` in the sdk-ops repo never sees it as
# a package; the Dockerfile renames it back before compiling.
COPY router/ ./router/
RUN mv router/main.go.txt router/main.go \
 && cd router && go mod init libsql-router && go mod tidy && go mod download \
 && CGO_ENABLED=0 GOOS=linux go build -o /router .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates docker-cli
COPY --from=builder /router /router
EXPOSE 8443
ENTRYPOINT ["/router"]
