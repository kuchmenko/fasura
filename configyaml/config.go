// Package configyaml loads source-centric Fasura configuration from YAML.
package configyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/kuchmenko/fasura"
	"gopkg.in/yaml.v3"
)

var environmentVariable = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

type fileConfig struct {
	Version int                  `yaml:"version"`
	Chains  map[string]fileChain `yaml:"chains"`
	Sources []fileSource         `yaml:"sources"`
}

type fileChain struct {
	ID  uint64 `yaml:"id"`
	RPC string `yaml:"rpc"`
}

type fileSource struct {
	Name        string                    `yaml:"name"`
	ABI         string                    `yaml:"abi"`
	Deployments map[string]fileDeployment `yaml:"deployments"`
	Events      []fileEvent               `yaml:"events"`
}

type fileDeployment struct {
	Addresses []string `yaml:"addresses"`
}

type fileEvent struct {
	Name  string             `yaml:"name"`
	Match []map[string][]any `yaml:"match"`
}

// Load reads a YAML file, expands exact ${VAR} connection and filter values,
// and loads every source ABI relative to the YAML file.
func Load(path string) (fasura.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fasura.Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	parsed, err := decode(data)
	if err != nil {
		return fasura.Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return build(parsed, filepath.Dir(path))
}

func decode(data []byte) (fileConfig, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var result fileConfig
	if err := decoder.Decode(&result); err != nil {
		return fileConfig{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fileConfig{}, errors.New("multiple YAML documents are not supported")
		}
		return fileConfig{}, err
	}
	return result, nil
}

func build(file fileConfig, baseDir string) (fasura.Config, error) {
	if file.Version != 1 {
		return fasura.Config{}, fmt.Errorf("unsupported version %d, want 1", file.Version)
	}
	result := fasura.Config{
		Chains:  make(map[string]fasura.Chain, len(file.Chains)),
		Sources: make([]fasura.Source, 0, len(file.Sources)),
	}
	for name, chain := range file.Chains {
		rpcURL, err := expand(chain.RPC)
		if err != nil {
			return fasura.Config{}, fmt.Errorf("chain %q rpc: %w", name, err)
		}
		result.Chains[name] = fasura.Chain{ID: chain.ID, RPCURL: rpcURL}
	}
	for _, source := range file.Sources {
		abiPath, err := expand(source.ABI)
		if err != nil {
			return fasura.Config{}, fmt.Errorf("source %q ABI: %w", source.Name, err)
		}
		if !filepath.IsAbs(abiPath) {
			abiPath = filepath.Join(baseDir, abiPath)
		}
		contractABI, err := loadABI(abiPath)
		if err != nil {
			return fasura.Config{}, fmt.Errorf("source %q: %w", source.Name, err)
		}
		configured := fasura.Source{
			Name:        source.Name,
			ABI:         contractABI,
			Deployments: make(map[string]fasura.Deployment, len(source.Deployments)),
			Events:      make([]fasura.EventConfig, 0, len(source.Events)),
		}
		for chainName, deployment := range source.Deployments {
			addresses := make([]common.Address, 0, len(deployment.Addresses))
			for _, rawAddress := range deployment.Addresses {
				address, err := expand(rawAddress)
				if err != nil {
					return fasura.Config{}, fmt.Errorf("source %q deployment %q address: %w", source.Name, chainName, err)
				}
				if !common.IsHexAddress(address) {
					return fasura.Config{}, fmt.Errorf("source %q deployment %q: invalid address %q", source.Name, chainName, address)
				}
				addresses = append(addresses, common.HexToAddress(address))
			}
			configured.Deployments[chainName] = fasura.Deployment{Addresses: addresses}
		}
		for _, event := range source.Events {
			matches := make([]fasura.Match, 0, len(event.Match))
			for _, clause := range event.Match {
				match := make(fasura.Match, len(clause))
				for field, values := range clause {
					expanded := make([]any, 0, len(values))
					for _, value := range values {
						value, err := expandValue(value)
						if err != nil {
							return fasura.Config{}, fmt.Errorf("source %q event %q match %q: %w", source.Name, event.Name, field, err)
						}
						expanded = append(expanded, value)
					}
					match[field] = expanded
				}
				matches = append(matches, match)
			}
			configured.Events = append(configured.Events, fasura.EventConfig{Name: event.Name, Match: matches})
		}
		result.Sources = append(result.Sources, configured)
	}
	return result, nil
}

func loadABI(path string) (abi.ABI, error) {
	file, err := os.Open(path)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("open ABI %q: %w", path, err)
	}
	defer file.Close()
	contractABI, err := abi.JSON(file)
	if err != nil {
		return abi.ABI{}, fmt.Errorf("parse ABI %q: %w", path, err)
	}
	return contractABI, nil
}

func expandValue(value any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return value, nil
	}
	return expand(text)
}

func expand(value string) (string, error) {
	match := environmentVariable.FindStringSubmatch(value)
	if match != nil {
		expanded, exists := os.LookupEnv(match[1])
		if !exists || expanded == "" {
			return "", fmt.Errorf("environment variable %s is required", match[1])
		}
		return expanded, nil
	}
	if strings.Contains(value, "${") {
		return "", fmt.Errorf("unsupported environment expression %q; use ${VAR} as the complete value", value)
	}
	return value, nil
}
