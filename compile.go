package fasura

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type compiledConfig struct {
	chains    map[string]compiledChain
	eventKeys map[string]struct{}
}

type compiledChain struct {
	name    string
	id      uint64
	rpcURL  string
	streams []compiledStream
}

type compiledStream struct {
	chainName string
	chainID   uint64
	source    string
	event     abi.Event
	queries   []ethereum.FilterQuery
	clauses   []topicClause
}

type topicClause map[int]map[common.Hash]struct{}

func compileConfig(config Config) (compiledConfig, error) {
	if len(config.Chains) == 0 {
		return compiledConfig{}, errors.New("at least one chain is required")
	}
	if len(config.Sources) == 0 {
		return compiledConfig{}, errors.New("at least one source is required")
	}

	result := compiledConfig{
		chains:    make(map[string]compiledChain, len(config.Chains)),
		eventKeys: make(map[string]struct{}),
	}
	chainNames := make([]string, 0, len(config.Chains))
	for name := range config.Chains {
		chainNames = append(chainNames, name)
	}
	sort.Strings(chainNames)
	for _, name := range chainNames {
		chain := config.Chains[name]
		if name == "" {
			return compiledConfig{}, errors.New("chain name is required")
		}
		if chain.ID == 0 {
			return compiledConfig{}, fmt.Errorf("chain %q: id is required", name)
		}
		if chain.RPCURL == "" {
			return compiledConfig{}, fmt.Errorf("chain %q: RPC URL is required", name)
		}
		result.chains[name] = compiledChain{name: name, id: chain.ID, rpcURL: chain.RPCURL}
	}

	sourceNames := make(map[string]struct{}, len(config.Sources))
	for sourceIndex, source := range config.Sources {
		if source.Name == "" {
			return compiledConfig{}, fmt.Errorf("source %d: name is required", sourceIndex)
		}
		if strings.Contains(source.Name, ".") {
			return compiledConfig{}, fmt.Errorf("source %q: name cannot contain '.'", source.Name)
		}
		if _, exists := sourceNames[source.Name]; exists {
			return compiledConfig{}, fmt.Errorf("duplicate source name %q", source.Name)
		}
		sourceNames[source.Name] = struct{}{}
		if len(source.Deployments) == 0 {
			return compiledConfig{}, fmt.Errorf("source %q: at least one deployment is required", source.Name)
		}
		if len(source.Events) == 0 {
			return compiledConfig{}, fmt.Errorf("source %q: at least one event is required", source.Name)
		}

		events, err := compileEvents(source)
		if err != nil {
			return compiledConfig{}, err
		}
		deploymentNames := make([]string, 0, len(source.Deployments))
		for chainName := range source.Deployments {
			deploymentNames = append(deploymentNames, chainName)
		}
		sort.Strings(deploymentNames)
		for _, chainName := range deploymentNames {
			deployment := source.Deployments[chainName]
			chain, ok := result.chains[chainName]
			if !ok {
				return compiledConfig{}, fmt.Errorf("source %q: deployment references unknown chain %q", source.Name, chainName)
			}
			addresses, err := uniqueAddresses(deployment.Addresses)
			if err != nil {
				return compiledConfig{}, fmt.Errorf("source %q deployment %q: %w", source.Name, chainName, err)
			}
			for _, event := range events {
				queries := make([]ethereum.FilterQuery, len(event.clauses))
				for index, clause := range event.clauses {
					queries[index] = ethereum.FilterQuery{
						Addresses: append([]common.Address(nil), addresses...),
						Topics:    queryTopics(event.definition, clause),
					}
				}
				chain.streams = append(chain.streams, compiledStream{
					chainName: chainName,
					chainID:   chain.id,
					source:    source.Name,
					event:     event.definition,
					queries:   queries,
					clauses:   event.clauses,
				})
				result.eventKeys[source.Name+"."+event.definition.Name] = struct{}{}
			}
			result.chains[chainName] = chain
		}
	}
	for _, name := range chainNames {
		if len(result.chains[name].streams) == 0 {
			return compiledConfig{}, fmt.Errorf("chain %q has no source deployment", name)
		}
	}

	return result, nil
}

type compiledEvent struct {
	definition abi.Event
	clauses    []topicClause
}

func compileEvents(source Source) ([]compiledEvent, error) {
	result := make([]compiledEvent, 0, len(source.Events))
	names := make(map[string]struct{}, len(source.Events))
	for _, configured := range source.Events {
		if configured.Name == "" {
			return nil, fmt.Errorf("source %q: event name is required", source.Name)
		}
		if _, exists := names[configured.Name]; exists {
			return nil, fmt.Errorf("source %q: duplicate event %q", source.Name, configured.Name)
		}
		names[configured.Name] = struct{}{}

		definition, ok := source.ABI.Events[configured.Name]
		if !ok {
			return nil, fmt.Errorf("source %q: event %q not found in ABI", source.Name, configured.Name)
		}
		if definition.Anonymous {
			return nil, fmt.Errorf("source %q event %q: anonymous events are not supported", source.Name, configured.Name)
		}
		clauses, err := compileMatches(definition, configured.Match)
		if err != nil {
			return nil, fmt.Errorf("source %q event %q: %w", source.Name, configured.Name, err)
		}
		result = append(result, compiledEvent{definition: definition, clauses: clauses})
	}
	return result, nil
}

func compileMatches(event abi.Event, matches []Match) ([]topicClause, error) {
	if len(matches) == 0 {
		return []topicClause{{}}, nil
	}

	indexed := make(map[string]int)
	topicIndex := 1
	for _, input := range event.Inputs {
		if input.Indexed {
			if input.Name != "" {
				indexed[input.Name] = topicIndex
			}
			topicIndex++
		}
	}

	result := make([]topicClause, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for clauseIndex, match := range matches {
		if len(match) == 0 {
			return nil, fmt.Errorf("match %d is empty", clauseIndex)
		}
		clause := make(topicClause, len(match))
		for name, values := range match {
			position, ok := indexed[name]
			if !ok {
				if input, exists := argumentByName(event.Inputs, name); exists && !input.Indexed {
					return nil, fmt.Errorf("match field %q is not indexed", name)
				}
				return nil, fmt.Errorf("unknown indexed match field %q", name)
			}
			if len(values) == 0 {
				return nil, fmt.Errorf("match field %q has no values", name)
			}
			input, _ := argumentByName(event.Inputs, name)
			hashes := make(map[common.Hash]struct{}, len(values))
			for _, value := range values {
				topic, err := topicForValue(input.Type, value)
				if err != nil {
					return nil, fmt.Errorf("match field %q value %v: %w", name, value, err)
				}
				hashes[topic] = struct{}{}
			}
			clause[position] = hashes
		}
		key := clauseKey(clause)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, clause)
	}
	return result, nil
}

func argumentByName(arguments abi.Arguments, name string) (abi.Argument, bool) {
	for _, argument := range arguments {
		if argument.Name == name {
			return argument, true
		}
	}
	return abi.Argument{}, false
}

func uniqueAddresses(addresses []common.Address) ([]common.Address, error) {
	if len(addresses) == 0 {
		return nil, errors.New("at least one address is required")
	}
	result := make([]common.Address, 0, len(addresses))
	seen := make(map[common.Address]struct{}, len(addresses))
	for _, address := range addresses {
		if address == (common.Address{}) {
			return nil, errors.New("zero address is not allowed")
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
	}
	return result, nil
}

func queryTopics(event abi.Event, clause topicClause) [][]common.Hash {
	last := 0
	for position := range clause {
		if position > last {
			last = position
		}
	}
	topics := make([][]common.Hash, last+1)
	topics[0] = []common.Hash{event.ID}
	for position, allowed := range clause {
		values := make([]common.Hash, 0, len(allowed))
		for value := range allowed {
			values = append(values, value)
		}
		sort.Slice(values, func(i, j int) bool {
			return strings.Compare(values[i].Hex(), values[j].Hex()) < 0
		})
		topics[position] = values
	}
	return topics
}

func clauseKey(clause topicClause) string {
	positions := make([]int, 0, len(clause))
	for position := range clause {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	var builder strings.Builder
	for _, position := range positions {
		builder.WriteString(strconv.Itoa(position))
		builder.WriteByte(':')
		values := make([]string, 0, len(clause[position]))
		for value := range clause[position] {
			values = append(values, value.Hex())
		}
		sort.Strings(values)
		for _, value := range values {
			builder.WriteString(value)
			builder.WriteByte(',')
		}
		builder.WriteByte(';')
	}
	return builder.String()
}

func topicForValue(valueType abi.Type, raw any) (common.Hash, error) {
	value, err := convertMatchValue(valueType, raw)
	if err != nil {
		return common.Hash{}, err
	}
	topics, err := abi.MakeTopics([]any{value})
	if err != nil {
		return common.Hash{}, err
	}
	return topics[0][0], nil
}

func convertMatchValue(valueType abi.Type, raw any) (any, error) {
	switch valueType.T {
	case abi.AddressTy:
		value, ok := raw.(string)
		if !ok || !common.IsHexAddress(value) {
			return nil, fmt.Errorf("expected hex address, got %v", raw)
		}
		return common.HexToAddress(value), nil
	case abi.BoolTy:
		value, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected boolean, got %T", raw)
		}
		return value, nil
	case abi.StringTy:
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", raw)
		}
		return value, nil
	case abi.BytesTy:
		return decodeHexBytes(raw)
	case abi.FixedBytesTy:
		bytes, err := decodeHexBytes(raw)
		if err != nil {
			return nil, err
		}
		if len(bytes) != valueType.Size {
			return nil, fmt.Errorf("expected %d bytes, got %d", valueType.Size, len(bytes))
		}
		value := reflect.New(valueType.GetType()).Elem()
		reflect.Copy(value, reflect.ValueOf(bytes))
		return value.Interface(), nil
	case abi.IntTy, abi.UintTy:
		return convertInteger(valueType, raw)
	default:
		return nil, fmt.Errorf("indexed type %s is not supported in match", valueType.String())
	}
}

func decodeHexBytes(raw any) ([]byte, error) {
	value, ok := raw.(string)
	if !ok || !strings.HasPrefix(value, "0x") {
		return nil, fmt.Errorf("expected 0x-prefixed bytes, got %v", raw)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return nil, fmt.Errorf("decode hex bytes: %w", err)
	}
	return decoded, nil
}

func convertInteger(valueType abi.Type, raw any) (any, error) {
	text, err := integerText(raw)
	if err != nil {
		return nil, err
	}
	value, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", text)
	}
	if valueType.T == abi.UintTy {
		if value.Sign() < 0 || value.BitLen() > valueType.Size {
			return nil, fmt.Errorf("value %s does not fit uint%d", value, valueType.Size)
		}
	} else {
		limit := new(big.Int).Lsh(big.NewInt(1), uint(valueType.Size-1))
		minimum := new(big.Int).Neg(limit)
		maximum := new(big.Int).Sub(new(big.Int).Set(limit), big.NewInt(1))
		if value.Cmp(minimum) < 0 || value.Cmp(maximum) > 0 {
			return nil, fmt.Errorf("value %s does not fit int%d", value, valueType.Size)
		}
	}
	target := valueType.GetType()
	if target == reflect.TypeFor[*big.Int]() {
		return value, nil
	}
	result := reflect.New(target).Elem()
	if valueType.T == abi.UintTy {
		result.SetUint(value.Uint64())
	} else {
		result.SetInt(value.Int64())
	}
	return result.Interface(), nil
}

func integerText(raw any) (string, error) {
	switch value := raw.(type) {
	case int:
		return strconv.Itoa(value), nil
	case int8:
		return strconv.FormatInt(int64(value), 10), nil
	case int16:
		return strconv.FormatInt(int64(value), 10), nil
	case int32:
		return strconv.FormatInt(int64(value), 10), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case uint:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(value), 10), nil
	case uint64:
		return strconv.FormatUint(value, 10), nil
	case *big.Int:
		if value == nil {
			return "", errors.New("integer is nil")
		}
		return value.String(), nil
	case string:
		return value, nil
	default:
		return "", fmt.Errorf("expected integer, got %T", raw)
	}
}
