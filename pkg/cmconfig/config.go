package cmconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// CriticalConfigKeys are the critical configuration paths required by cm-connect.
	// Governing: REQ-0002, SPEC-cm-batch-runner
	CriticalConfigKeys = []string{
		"scan.extensions.include",
		"output.format",
		"tools.confirm_commands",
		"tools.confirm_writes",
	}
)

// findMappingChild searches a MappingNode for a child node matching key.
func findMappingChild(mappingNode *yaml.Node, key string) (*yaml.Node, error) {
	if mappingNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got kind %v", mappingNode.Kind)
	}

	for i := 0; i < len(mappingNode.Content); i += 2 {
		keyNode := mappingNode.Content[i]
		if keyNode.Value == key {
			if i+1 < len(mappingNode.Content) {
				return mappingNode.Content[i+1], nil
			}
			return nil, fmt.Errorf("mapping key %q has no associated value", key)
		}
	}
	return nil, nil
}

// lookupPath traverses a YAML document tree following dot-separated path components.
func lookupPath(rootMapping *yaml.Node, path string) (*yaml.Node, error) {
	parts := strings.Split(path, ".")
	current := rootMapping

	for i, part := range parts {
		child, err := findMappingChild(current, part)
		if err != nil {
			return nil, fmt.Errorf("error resolving %q at segment %q: %w", path, part, err)
		}
		if child == nil {
			return nil, fmt.Errorf("critical configuration key %q is missing or relocated", path)
		}
		if i == len(parts)-1 {
			return child, nil
		}
		if child.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("critical configuration key %q: intermediate segment %q is not a mapping node", path, part)
		}
		current = child
	}

	return current, nil
}

// MutateConfig takes a raw YAML byte slice, validates presence of critical keys,
// and applies headless default overrides in-place using YAML AST manipulation.
// Governing: REQ-0002, SPEC-cm-batch-runner
func MutateConfig(yamlBytes []byte) ([]byte, error) {
	if len(bytes.TrimSpace(yamlBytes)) == 0 {
		return nil, errors.New("empty YAML document")
	}

	var root yaml.Node
	if err := yaml.Unmarshal(yamlBytes, &root); err != nil {
		return nil, fmt.Errorf("failed to parse YAML document: %w", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, errors.New("invalid or empty YAML document root")
	}

	rootMapping := root.Content[0]
	if rootMapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("root node is not a mapping (got kind %v)", rootMapping.Kind)
	}

	// 1. Validate and mutate scan.extensions.include
	includeNode, err := lookupPath(rootMapping, "scan.extensions.include")
	if err != nil {
		return nil, err
	}
	if includeNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("critical configuration key 'scan.extensions.include' must be a sequence node (got kind %v)", includeNode.Kind)
	}

	hasRS := false
	for _, item := range includeNode.Content {
		if item.Value == ".rs" {
			hasRS = true
			break
		}
	}
	if !hasRS {
		rsNode := &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: ".rs",
		}
		includeNode.Content = append(includeNode.Content, rsNode)
	}

	// 2. Validate and mutate output.format
	formatNode, err := lookupPath(rootMapping, "output.format")
	if err != nil {
		return nil, err
	}
	formatNode.Kind = yaml.ScalarNode
	formatNode.Tag = "!!str"
	formatNode.Value = "json"

	// 3. Validate and mutate tools.confirm_commands
	confirmCmdNode, err := lookupPath(rootMapping, "tools.confirm_commands")
	if err != nil {
		return nil, err
	}
	confirmCmdNode.Kind = yaml.ScalarNode
	confirmCmdNode.Tag = "!!bool"
	confirmCmdNode.Value = "false"

	// 4. Validate and mutate tools.confirm_writes
	confirmWriteNode, err := lookupPath(rootMapping, "tools.confirm_writes")
	if err != nil {
		return nil, err
	}
	confirmWriteNode.Kind = yaml.ScalarNode
	confirmWriteNode.Tag = "!!bool"
	confirmWriteNode.Value = "false"

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, fmt.Errorf("failed to encode mutated YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize YAML encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// MutateConfigFile reads a YAML configuration file from path, mutates it in-place,
// and writes the updated content back to the file.
// Governing: REQ-0002, SPEC-cm-batch-runner
func MutateConfigFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to inspect config file %q: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	mutated, err := MutateConfig(data)
	if err != nil {
		return fmt.Errorf("failed to mutate config file %q: %w", path, err)
	}

	if err := os.WriteFile(path, mutated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write mutated config to %q: %w", path, err)
	}

	return nil
}

// DefaultConfigPath returns the default CodeMender configuration file path
// ($HOME/.codemender/config.yaml).
// Governing: REQ-0002, SPEC-cm-batch-runner
func DefaultConfigPath() (string, error) {
	home := os.Getenv("HOME")
	if strings.TrimSpace(home) == "" {
		return "", errors.New("HOME environment variable is not set")
	}
	return filepath.Join(home, ".codemender", "config.yaml"), nil
}
