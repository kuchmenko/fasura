package configyaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/kuchmenko/fasura"
)

const testABI = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`

func TestLoadResolvesRelativeABIAndExpandsEnvironment(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "abis", "IERC20.json"), testABI)
	t.Setenv("TEST_RPC_URL", "ws://example.test")
	t.Setenv("TEST_WALLET", "0x2000000000000000000000000000000000000002")
	path := filepath.Join(directory, "fasura.yaml")
	writeFile(t, path, `version: 1
chains:
  ethereum:
    id: 1
    rpc: "${TEST_RPC_URL}"
sources:
  - name: token
    abi: ./abis/IERC20.json
    deployments:
      ethereum:
        addresses:
          - "0x1000000000000000000000000000000000000001"
    events:
      - name: Transfer
        match:
          - from: ["${TEST_WALLET}"]
`)

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Chains["ethereum"].RPCURL != "ws://example.test" {
		t.Fatalf("RPC URL = %q", config.Chains["ethereum"].RPCURL)
	}
	if _, ok := config.Sources[0].ABI.Events["Transfer"]; !ok {
		t.Fatal("Transfer event not loaded from relative ABI")
	}
	if got := config.Sources[0].Events[0].Match[0]["from"][0]; got != "0x2000000000000000000000000000000000000002" {
		t.Fatalf("expanded wallet = %v", got)
	}
	if got := config.Sources[0].Deployments["ethereum"].Addresses[0]; got != common.HexToAddress("0x1000000000000000000000000000000000000001") {
		t.Fatalf("deployment address = %s", got)
	}
}

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fasura.yaml")
	writeFile(t, path, `version: 1
chains:
  ethereum:
    id: 1
    rpc: ws://example.test
    retry: true
sources: []
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field retry not found") {
		t.Fatalf("error = %v, want unknown field error", err)
	}
}

func TestLoadRejectsMissingEnvironmentVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fasura.yaml")
	writeFile(t, path, `version: 1
chains:
  ethereum:
    id: 1
    rpc: "${FASURA_TEST_MISSING_RPC}"
sources: []
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "environment variable FASURA_TEST_MISSING_RPC is required") {
		t.Fatalf("error = %v, want missing environment variable error", err)
	}
}

func TestLoadCompilesBooleanMatchFromEnvironment(t *testing.T) {
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "contract.json"), `[{"anonymous":false,"inputs":[{"indexed":true,"name":"enabled","type":"bool"}],"name":"Changed","type":"event"}]`)
	t.Setenv("TEST_ENABLED", "true")
	path := filepath.Join(directory, "fasura.yaml")
	writeFile(t, path, `version: 1
chains:
  ethereum:
    id: 1
    rpc: ws://example.test
sources:
  - name: contract
    abi: ./contract.json
    deployments:
      ethereum:
        addresses: ["0x1000000000000000000000000000000000000001"]
    events:
      - name: Changed
        match:
          - enabled: ["${TEST_ENABLED}"]
`)

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fasura.New(config); err != nil {
		t.Fatalf("compile config: %v", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
