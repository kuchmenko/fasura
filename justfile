set dotenv-load := true

run:
  go run ./examples/ctf-listener -config fasura.toml

test: 
  go test ./...

vet:
  go vet ./...
