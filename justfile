set dotenv-load := true

run:
  go run ./examples/ctf -config examples/ctf/fasura.yaml

test:
  go test -race ./...

vet:
  go vet ./...
