package cmconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultOverrides defines the required headless configuration key-value mappings.
// Scalar values overwrite the target node, while slice values append elements idempotently.
// If any critical key path is missing or relocated, mutation fails fast with an error.
// Governing: REQ-0002, SPEC-cm-batch-runner
var DefaultOverrides = map[string]any{
	"scan.extensions.include": []string{".rs"},
	"output.format":           "json",
	"tools.confirm_commands":  false,
	"tools.confirm_writes":    false,
}

// findMappingChild searches a MappingNode for a child value node matching key.
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

// sequenceContains checks whether a SequenceNode already contains the given scalar string value.
func sequenceContains(seqNode *yaml.Node, val string) bool {
	for _, item := range seqNode.Content {
		if item.Value == val {
			return true
		}
	}
	return false
}

// applyValue updates a target AST node with the specified value.
func applyValue(node *yaml.Node, path string, val any) error {
	switch v := val.(type) {
	case []string:
		if node.Kind != yaml.SequenceNode {
			return fmt.Errorf("critical configuration key %q must be a sequence node (got kind %v)", path, node.Kind)
		}
		for _, elem := range v {
			if !sequenceContains(node, elem) {
				node.Content = append(node.Content, &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: elem,
				})
			}
		}
	case string:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!str"
		node.Value = v
	case bool:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!bool"
		node.Value = strconv.FormatBool(v)
	case int:
		node.Kind = yaml.ScalarNode
		node.Tag = "!!int"
		node.Value = strconv.Itoa(v)
	default:
		return fmt.Errorf("unsupported override value type %T for key %q", val, path)
	}
	return nil
}

// ApplyOverrides applies a dictionary of key-path to value mappings to the YAML AST.
// For each key-value pair, it traverses to the target node and updates it in-place.
// If any key does not exist in the document, it fails fast and returns an error.
// Governing: REQ-0002, SPEC-cm-batch-runner
func ApplyOverrides(root *yaml.Node, overrides map[string]any) error {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return errors.New("invalid or empty YAML document root")
	}

	rootMapping := root.Content[0]
	if rootMapping.Kind != yaml.MappingNode {
		return fmt.Errorf("root node is not a mapping (got kind %v)", rootMapping.Kind)
	}

	// Iterate deterministically by sorting keys
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, path := range keys {
		val := overrides[path]
		node, err := lookupPath(rootMapping, path)
		if err != nil {
			return err
		}
		if err := applyValue(node, path, val); err != nil {
			return err
		}
	}
	return nil
}

// MutateConfig takes a raw YAML byte slice and applies DefaultOverrides in-place.
// Governing: REQ-0002, SPEC-cm-batch-runner
func MutateConfig(yamlBytes []byte) ([]byte, error) {
	return MutateConfigWithOverrides(yamlBytes, DefaultOverrides)
}

// MutateConfigWithOverrides takes a raw YAML byte slice and applies the given overrides in-place.
// Governing: REQ-0002, SPEC-cm-batch-runner
func MutateConfigWithOverrides(yamlBytes []byte, overrides map[string]any) ([]byte, error) {
	if len(bytes.TrimSpace(yamlBytes)) == 0 {
		return nil, errors.New("empty YAML document")
	}

	var root yaml.Node
	if err := yaml.Unmarshal(yamlBytes, &root); err != nil {
		return nil, fmt.Errorf("failed to parse YAML document: %w", err)
	}

	if err := ApplyOverrides(&root, overrides); err != nil {
		return nil, err
	}

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

// MutateConfigFile reads a YAML configuration file from path, mutates it in-place using DefaultOverrides,
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

// AppendScanExtension reads the configuration file at path, appends ext to scan.extensions.include
// idempotently, and writes the updated content back to the file.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
func AppendScanExtension(path string, ext string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to inspect config file %q: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	overrides := map[string]any{
		"scan.extensions.include": []string{ext},
	}
	mutated, err := MutateConfigWithOverrides(data, overrides)
	if err != nil {
		return fmt.Errorf("failed to mutate config file %q: %w", path, err)
	}

	if err := os.WriteFile(path, mutated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write mutated config to %q: %w", path, err)
	}

	return nil
}

// EnsureDiffExtension ensures that ".diff" is registered in scan.extensions.include.
// If path is specified, that configuration file is mutated; otherwise DefaultConfigPath() is used.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
func EnsureDiffExtension(path ...string) error {
	targetPath := ""
	if len(path) > 0 && strings.TrimSpace(path[0]) != "" {
		targetPath = strings.TrimSpace(path[0])
	} else {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return err
		}
		targetPath = defaultPath
	}
	return AppendScanExtension(targetPath, ".diff")
}
