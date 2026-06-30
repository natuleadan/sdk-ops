FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN CGO_ENABLED=0 go build -o /app .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app /app
EXPOSE 8080
CMD ["/app"]
