CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ./dev/lb ./cmd/main.go
chmod +x ./dev/lb