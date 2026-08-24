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

// Config represents a CodeMender configuration document loaded in memory.
// It wraps a yaml.Node AST document tree to preserve comments, indentation,
// and unmanaged upstream fields during mutations.
// Governing: ADR-0001, ADR-0007, SPEC-cm-batch-runner
type Config struct {
	root yaml.Node
	path string
}

// LoadConfig reads and parses the CodeMender configuration file from DefaultConfigPath().
// Governing: ADR-0001, ADR-0007, SPEC-cm-batch-runner
func LoadConfig() (*Config, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadConfigFile(path)
}

// LoadConfigFile reads and parses a YAML configuration file from the specified path.
// Governing: ADR-0001, ADR-0007, SPEC-cm-batch-runner
func LoadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

// ParseConfig parses a raw YAML byte slice into a Config object.
// Governing: ADR-0001, ADR-0007, SPEC-cm-batch-runner
func ParseConfig(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("empty YAML document")
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse YAML document: %w", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, errors.New("invalid or empty YAML document root")
	}
	if root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("root node is not a mapping (got kind %v)", root.Content[0].Kind)
	}

	return &Config{root: root}, nil
}

// ApplyOverrides applies a dictionary of key-path to value mappings to the YAML AST in-place.
// Scalar values overwrite the target node, while slice values append elements idempotently.
// If any critical key path is missing or relocated, it fails fast and returns an error.
// Governing: REQ-0002, ADR-0007, SPEC-cm-batch-runner
func (c *Config) ApplyOverrides(overrides map[string]any) error {
	if c.root.Kind != yaml.DocumentNode || len(c.root.Content) == 0 {
		return errors.New("invalid or empty YAML document root")
	}

	rootMapping := c.root.Content[0]
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

// AppendExtension appends ext to scan.extensions.include idempotently.
// It guarantees that duplicate extension entries are not added if already present.
// Governing: ADR-0007, SPEC-cm-batch-runner, REQ-0014
func (c *Config) AppendExtension(ext string) error {
	return c.ApplyOverrides(map[string]any{
		"scan.extensions.include": []string{ext},
	})
}

// Bytes serializes the mutated YAML document back to formatted bytes.
// Governing: REQ-0002, ADR-0007, SPEC-cm-batch-runner
func (c *Config) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&c.root); err != nil {
		return nil, fmt.Errorf("failed to encode mutated YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize YAML encoder: %w", err)
	}
	return buf.Bytes(), nil
}

// Write writes the serialized YAML document back to disk at the config's path.
// If the config was not loaded from a file, it writes to DefaultConfigPath().
// Governing: REQ-0002, ADR-0007, SPEC-cm-batch-runner
func (c *Config) Write() error {
	dest := c.path
	if dest == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return err
		}
		dest = defaultPath
	}

	data, err := c.Bytes()
	if err != nil {
		return err
	}

	perm := os.FileMode(0o644)
	if info, err := os.Stat(dest); err == nil {
		perm = info.Mode().Perm()
	}

	if err := os.WriteFile(dest, data, perm); err != nil {
		return fmt.Errorf("failed to write mutated config to %q: %w", dest, err)
	}
	c.path = dest
	return nil
}

// Path returns the file path associated with the loaded Config, if any.
func (c *Config) Path() string {
	return c.path
}

// ApplyOverrides loads the default CodeMender configuration file, applies overrides in-place, and writes it back.
// Governing: REQ-0002, ADR-0007, SPEC-cm-batch-runner
func ApplyOverrides(overrides map[string]any) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	if err := cfg.ApplyOverrides(overrides); err != nil {
		return err
	}
	return cfg.Write()
}

// ApplyDefaultOverrides loads the default CodeMender configuration file, applies DefaultOverrides in-place, and writes it back.
// Governing: REQ-0002, SPEC-cm-batch-runner
func ApplyDefaultOverrides() error {
	return ApplyOverrides(DefaultOverrides)
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
