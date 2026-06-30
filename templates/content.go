package templates

var htmlNginxConf = `server {
    listen 80;
    server_name _;
    root /usr/share/nginx/html;
    index index.html;
    location / {
        try_files $uri $uri/ =404;
    }
}`

var htmlIndex = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SDK Ops - Deployed</title>
    <style>
        body { font-family: system-ui, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #0f172a; color: #e2e8f0; }
        .card { text-align: center; padding: 2rem; }
        h1 { font-size: 2.5rem; margin-bottom: 0.5rem; }
        p { color: #94a3b8; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Deployed with SDK Ops</h1>
        <p>Static site via sdk-ops deploy push</p>
    </div>
</body>
</html>`

var htmlCompose = `version: "3.8"
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
      - ./:/usr/share/nginx/html:ro
`

var nodePackageJSON = `{
  "name": "app",
  "version": "1.0.0",
  "main": "server.js",
  "scripts": {
    "start": "node server.js"
  },
  "dependencies": {
    "express": "^4"
  }
}`

var nodeServerJS = `const express = require("express");
const app = express();
const port = process.env.PORT || 3000;

app.get("/", (req, res) => {
  res.json({ status: "ok", service: process.env.SERVICE_NAME || "app" });
});

app.get("/health", (req, res) => {
  res.json({ status: "healthy" });
});

app.listen(port, () => {
  console.log("Listening on port", port);
});`

var nodeDockerfile = `FROM node:20-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --production
COPY . .
EXPOSE 3000
CMD ["node", "server.js"]`

var wpCompose = `version: "3.8"
services:
  db:
    image: mysql:8
    volumes:
      - db_data:/var/lib/mysql
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-rootpass}
      MYSQL_DATABASE: ${MYSQL_DATABASE:-wordpress}
      MYSQL_USER: ${MYSQL_USER:-wpuser}
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-wppass}
    restart: always
  wordpress:
    image: wordpress:latest
    ports:
      - "80:80"
    environment:
      WORDPRESS_DB_HOST: db:3306
      WORDPRESS_DB_USER: ${MYSQL_USER:-wpuser}
      WORDPRESS_DB_PASSWORD: ${MYSQL_PASSWORD:-wppass}
      WORDPRESS_DB_NAME: ${MYSQL_DATABASE:-wordpress}
    volumes:
      - wp_data:/var/www/html
    depends_on:
      - db
    restart: always
volumes:
  db_data:
  wp_data:
`

var wpServiceYAML = `name: my-wordpress
registry: ewr.vultrcr.com/nlaregistry
ports:
  - "80:80"
health:
  path: /wp-admin/install.php
  interval: 60
env:
  MYSQL_ROOT_PASSWORD: rootpass
  MYSQL_DATABASE: wordpress
  MYSQL_USER: wpuser
  MYSQL_PASSWORD: wppass
`

var goDockerfile = `FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN CGO_ENABLED=0 go build -o /app .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app /app
EXPOSE 8080
CMD ["/app"]`

var goMainGo = `package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := os.Getenv("SERVICE_NAME")
		if name == "" {
			name = "app"
		}
		fmt.Fprintf(w, `+"`"+`{"status":"ok","service":"%s"}`+"`"+`, name)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `+"`"+`{"status":"healthy"}`+"`"+`)
	})

	log.Printf("Listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}`

var goGoMod = `module app

go 1.26
`
