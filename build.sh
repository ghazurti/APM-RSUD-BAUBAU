#!/bin/sh
# Build CSS Tailwind (file lokal, tanpa CDN) lalu apm.exe untuk Windows server.
set -e
cd "$(dirname "$0")"
npx -y tailwindcss@3.4.17 -i web/tailwind/input.css -o web/static/app.css --minify
go test ./internal/...
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w" -o dist/apm.exe ./cmd/apm
go build -ldflags "-s -w" -o dist/apm ./cmd/apm
# apmagent.exe untuk PC kiosk (tanpa jendela console; log di logs\apmagent.log)
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H windowsgui" -o dist/apmagent.exe ./cmd/apmagent
echo "OK → dist/apm.exe, dist/apmagent.exe"
