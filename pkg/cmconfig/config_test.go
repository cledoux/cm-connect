package cmconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleValidConfig = `# CodeMender Default Configuration
version: "1.0"
server:
  host: "127.0.0.1"
  port: 8080
scan:
  max_file_size_kb: 1024
  extensions:
    include:
      - ".go"
      - ".py"
      - ".js"
    exclude:
      - ".min.js"
output:
  format: "table"
  color: true
tools:
  confirm_commands: true
  confirm_writes: true
  timeout_seconds: 300
`

func TestMutateConfig_ValidConfig(t *testing.T) {
	mutated, err := MutateConfig([]byte(sampleValidConfig))
	if err != nil {
		t.Fatalf("unexpected error mutating config: %v", err)
	}

	var parsed struct {
		Version string `yaml:"version"`
		Server  struct {
			Host string `yaml:"host"`
			Port int    `yaml:"port"`
		} `yaml:"server"`
		Scan struct {
			MaxFileSizeKb int `yaml:"max_file_size_kb"`
			Extensions    struct {
				Include []string `yaml:"include"`
				Exclude []string `yaml:"exclude"`
			} `yaml:"extensions"`
		} `yaml:"scan"`
		Output struct {
			Format string `yaml:"format"`
			Color  bool   `yaml:"color"`
		} `yaml:"output"`
		Tools struct {
			ConfirmCommands bool `yaml:"confirm_commands"`
			ConfirmWrites   bool `yaml:"confirm_writes"`
			TimeoutSeconds  int  `yaml:"timeout_seconds"`
		} `yaml:"tools"`
	}

	if err := yaml.Unmarshal(mutated, &parsed); err != nil {
		t.Fatalf("failed to unmarshal mutated YAML: %v", err)
	}

	// Verify scan.extensions.include contains .rs
	foundRS := false
	for _, ext := range parsed.Scan.Extensions.Include {
		if ext == ".rs" {
			foundRS = true
			break
		}
	}
	if !foundRS {
		t.Errorf("expected .rs to be in scan.extensions.include, got %v", parsed.Scan.Extensions.Include)
	}

	// Verify original extensions preserved
	expectedIncludes := []string{".go", ".py", ".js", ".rs"}
	if len(parsed.Scan.Extensions.Include) != len(expectedIncludes) {
		t.Errorf("expected %d includes, got %d: %v", len(expectedIncludes), len(parsed.Scan.Extensions.Include), parsed.Scan.Extensions.Include)
	}

	// Verify output.format set to json
	if parsed.Output.Format != "json" {
		t.Errorf("expected output.format to be 'json', got %q", parsed.Output.Format)
	}

	// Verify tools.confirm_commands set to false
	if parsed.Tools.ConfirmCommands {
		t.Errorf("expected tools.confirm_commands to be false, got true")
	}

	// Verify tools.confirm_writes set to false
	if parsed.Tools.ConfirmWrites {
		t.Errorf("expected tools.confirm_writes to be false, got true")
	}

	// Verify unmanaged keys preserved
	if parsed.Version != "1.0" {
		t.Errorf("expected version '1.0', got %q", parsed.Version)
	}
	if parsed.Server.Port != 8080 {
		t.Errorf("expected server.port 8080, got %d", parsed.Server.Port)
	}
	if parsed.Scan.MaxFileSizeKb != 1024 {
		t.Errorf("expected scan.max_file_size_kb 1024, got %d", parsed.Scan.MaxFileSizeKb)
	}
	if parsed.Tools.TimeoutSeconds != 300 {
		t.Errorf("expected tools.timeout_seconds 300, got %d", parsed.Tools.TimeoutSeconds)
	}

	// Verify comments preserved in mutated YAML string
	mutatedStr := string(mutated)
	if !strings.Contains(mutatedStr, "# CodeMender Default Configuration") {
		t.Errorf("expected comments to be preserved in mutated output, got:\n%s", mutatedStr)
	}
}

func TestMutateConfig_Idempotence(t *testing.T) {
	configWithRS := `
scan:
  extensions:
    include:
      - ".go"
      - ".rs"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`
	mutated, err := MutateConfig([]byte(configWithRS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second mutation should not duplicate .rs
	mutatedAgain, err := MutateConfig(mutated)
	if err != nil {
		t.Fatalf("unexpected error on second mutation: %v", err)
	}

	var parsed struct {
		Scan struct {
			Extensions struct {
				Include []string `yaml:"include"`
			} `yaml:"extensions"`
		} `yaml:"scan"`
	}

	if err := yaml.Unmarshal(mutatedAgain, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	rsCount := 0
	for _, ext := range parsed.Scan.Extensions.Include {
		if ext == ".rs" {
			rsCount++
		}
	}
	if rsCount != 1 {
		t.Errorf("expected exactly 1 instance of .rs in scan.extensions.include, got %d: %v", rsCount, parsed.Scan.Extensions.Include)
	}
}

func TestMutateConfig_MissingCriticalKeys(t *testing.T) {
	tests := []struct {
		name        string
		inputYAML   string
		expectedErr string
	}{
		{
			name: "missing scan.extensions.include",
			inputYAML: `
scan:
  max_file_size_kb: 1024
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`,
			expectedErr: "scan.extensions.include",
		},
		{
			name: "missing output.format",
			inputYAML: `
scan:
  extensions:
    include:
      - ".go"
output:
  color: true
tools:
  confirm_commands: true
  confirm_writes: true
`,
			expectedErr: "output.format",
		},
		{
			name: "missing tools.confirm_commands",
			inputYAML: `
scan:
  extensions:
    include:
      - ".go"
output:
  format: "table"
tools:
  confirm_writes: true
`,
			expectedErr: "tools.confirm_commands",
		},
		{
			name: "missing tools.confirm_writes",
			inputYAML: `
scan:
  extensions:
    include:
      - ".go"
output:
  format: "table"
tools:
  confirm_commands: true
`,
			expectedErr: "tools.confirm_writes",
		},
		{
			name: "missing top-level tools section entirely",
			inputYAML: `
scan:
  extensions:
    include:
      - ".go"
output:
  format: "table"
`,
			expectedErr: "tools.confirm_commands",
		},
		{
			name: "scan.extensions.include is not a sequence",
			inputYAML: `
scan:
  extensions:
    include: ".go"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`,
			expectedErr: "scan.extensions.include",
		},
		{
			name:        "empty YAML document",
			inputYAML:   ``,
			expectedErr: "empty",
		},
		{
			name: "malformed YAML document",
			inputYAML: `
scan: [invalid yaml {
`,
			expectedErr: "yaml",
		},
		{
			name: "non-mapping root node",
			inputYAML: `- item1
- item2
`,
			expectedErr: "mapping",
		},
		{
			name: "scan is a scalar instead of mapping",
			inputYAML: `
scan: "not-a-map"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`,
			expectedErr: "not a mapping node",
		},
		{
			name: "scan.extensions is a scalar instead of mapping",
			inputYAML: `
scan:
  extensions: "not-a-map"
output:
  format: "table"
tools:
  confirm_commands: true
  confirm_writes: true
`,
			expectedErr: "not a mapping node",
		},
		{
			name: "document only comment or null",
			inputYAML: `
# only comment
`,
			expectedErr: "empty",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MutateConfig([]byte(tc.inputYAML))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.expectedErr)) {
				t.Errorf("expected error message to contain %q, got: %q", tc.expectedErr, err.Error())
			}
		})
	}
}

func TestMutateConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := MutateConfigFile(configPath); err != nil {
		t.Fatalf("unexpected error mutating file: %v", err)
	}

	mutatedContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read mutated file: %v", err)
	}

	mutatedStr := string(mutatedContent)
	if !strings.Contains(mutatedStr, `format: "json"`) && !strings.Contains(mutatedStr, `format: json`) {
		t.Errorf("expected mutated file to contain format: json, got:\n%s", mutatedStr)
	}
	if !strings.Contains(mutatedStr, ".rs") {
		t.Errorf("expected mutated file to contain .rs, got:\n%s", mutatedStr)
	}
	if !strings.Contains(mutatedStr, "confirm_commands: false") {
		t.Errorf("expected mutated file to contain confirm_commands: false, got:\n%s", mutatedStr)
	}
	if !strings.Contains(mutatedStr, "confirm_writes: false") {
		t.Errorf("expected mutated file to contain confirm_writes: false, got:\n%s", mutatedStr)
	}
}

func TestMutateConfigFile_Errors(t *testing.T) {
	t.Run("non-existent file", func(t *testing.T) {
		err := MutateConfigFile("/non/existent/path/config.yaml")
		if err == nil {
			t.Errorf("expected error for non-existent file, got nil")
		}
	})

	t.Run("invalid YAML file", func(t *testing.T) {
		tempDir := t.TempDir()
		badPath := filepath.Join(tempDir, "bad.yaml")
		if err := os.WriteFile(badPath, []byte("scan: [invalid"), 0o600); err != nil {
			t.Fatalf("failed to write bad file: %v", err)
		}
		err := MutateConfigFile(badPath)
		if err == nil {
			t.Errorf("expected error for invalid YAML file, got nil")
		}
	})
}

func TestMutateConfigWithOverrides(t *testing.T) {
	customConfig := `
server:
  port: 8080
scan:
  timeout: 10
  extensions:
    include:
      - ".go"
`
	overrides := map[string]any{
		"server.port":             9090,
		"scan.timeout":            60,
		"scan.extensions.include": []string{".py", ".rs"},
	}

	mutated, err := MutateConfigWithOverrides([]byte(customConfig), overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		Server struct {
			Port int `yaml:"port"`
		} `yaml:"server"`
		Scan struct {
			Timeout    int `yaml:"timeout"`
			Extensions struct {
				Include []string `yaml:"include"`
			} `yaml:"extensions"`
		} `yaml:"scan"`
	}

	if err := yaml.Unmarshal(mutated, &parsed); err != nil {
		t.Fatalf("failed to unmarshal mutated YAML: %v", err)
	}

	if parsed.Server.Port != 9090 {
		t.Errorf("expected server.port 9090, got %d", parsed.Server.Port)
	}
	if parsed.Scan.Timeout != 60 {
		t.Errorf("expected scan.timeout 60, got %d", parsed.Scan.Timeout)
	}
	expectedIncludes := []string{".go", ".py", ".rs"}
	if len(parsed.Scan.Extensions.Include) != len(expectedIncludes) {
		t.Errorf("expected %d includes, got %v", len(expectedIncludes), parsed.Scan.Extensions.Include)
	}

	t.Run("unsupported type error", func(t *testing.T) {
		badOverrides := map[string]any{
			"server.port": struct{}{},
		}
		_, err := MutateConfigWithOverrides([]byte(customConfig), badOverrides)
		if err == nil {
			t.Errorf("expected error for unsupported override value type, got nil")
		}
	})
}

func TestDefaultConfigPath(t *testing.T) {
	// Test DefaultConfigPath with HOME set
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	os.Setenv("HOME", "/custom/home")
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("/custom/home", ".codemender", "config.yaml")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}

	// Test with HOME unset
	os.Unsetenv("HOME")
	_, err = DefaultConfigPath()
	if err == nil {
		t.Errorf("expected error when HOME is unset, got nil")
	}
}
