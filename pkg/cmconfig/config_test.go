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

func TestLoadConfig(t *testing.T) {
	t.Run("loads from DefaultConfigPath", func(t *testing.T) {
		tempDir := t.TempDir()
		origHome := os.Getenv("HOME")
		defer os.Setenv("HOME", origHome)
		os.Setenv("HOME", tempDir)

		cmDir := filepath.Join(tempDir, ".codemender")
		if err := os.MkdirAll(cmDir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		configPath := filepath.Join(cmDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected LoadConfig error: %v", err)
		}
		if cfg.Path() != configPath {
			t.Errorf("expected path %q, got %q", configPath, cfg.Path())
		}
	})

	t.Run("returns error when HOME is unset", func(t *testing.T) {
		origHome := os.Getenv("HOME")
		defer os.Setenv("HOME", origHome)
		os.Unsetenv("HOME")

		_, err := LoadConfig()
		if err == nil {
			t.Errorf("expected error when HOME is unset, got nil")
		}
	})
}

func TestLoadConfigFile(t *testing.T) {
	t.Run("loads from explicit path", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "custom_config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected LoadConfigFile error: %v", err)
		}
		if cfg.Path() != configPath {
			t.Errorf("expected path %q, got %q", configPath, cfg.Path())
		}
	})

	t.Run("error on non-existent file", func(t *testing.T) {
		_, err := LoadConfigFile("/non/existent/path/config.yaml")
		if err == nil {
			t.Errorf("expected error for non-existent file, got nil")
		}
	})

	t.Run("error on invalid YAML file", func(t *testing.T) {
		tempDir := t.TempDir()
		badPath := filepath.Join(tempDir, "bad.yaml")
		if err := os.WriteFile(badPath, []byte("scan: [invalid"), 0o600); err != nil {
			t.Fatalf("failed to write bad file: %v", err)
		}
		_, err := LoadConfigFile(badPath)
		if err == nil {
			t.Errorf("expected error for invalid YAML, got nil")
		}
	})
}

func TestParseConfig(t *testing.T) {
	t.Run("valid YAML bytes", func(t *testing.T) {
		cfg, err := ParseConfig([]byte(sampleValidConfig))
		if err != nil {
			t.Fatalf("unexpected ParseConfig error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil Config")
		}
	})

	t.Run("empty YAML bytes", func(t *testing.T) {
		_, err := ParseConfig([]byte(""))
		if err == nil {
			t.Errorf("expected error for empty YAML, got nil")
		}
	})

	t.Run("malformed YAML bytes", func(t *testing.T) {
		_, err := ParseConfig([]byte("scan: [broken"))
		if err == nil {
			t.Errorf("expected error for malformed YAML, got nil")
		}
	})

	t.Run("non-mapping root node", func(t *testing.T) {
		_, err := ParseConfig([]byte("- item1\n- item2\n"))
		if err == nil {
			t.Errorf("expected error for sequence root node, got nil")
		}
	})
}

func TestConfig_ApplyOverrides(t *testing.T) {
	t.Run("applies scalar and sequence overrides", func(t *testing.T) {
		cfg, err := ParseConfig([]byte(sampleValidConfig))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"server.port":             9090,
			"scan.max_file_size_kb":   2048,
			"scan.extensions.include": []string{".rs", ".diff"},
			"output.format":           "json",
			"tools.confirm_commands":  false,
		})
		if err != nil {
			t.Fatalf("unexpected ApplyOverrides error: %v", err)
		}

		bytesOut, err := cfg.Bytes()
		if err != nil {
			t.Fatalf("unexpected Bytes error: %v", err)
		}

		var parsed struct {
			Server struct {
				Port int `yaml:"port"`
			} `yaml:"server"`
			Scan struct {
				MaxFileSizeKb int `yaml:"max_file_size_kb"`
				Extensions    struct {
					Include []string `yaml:"include"`
				} `yaml:"extensions"`
			} `yaml:"scan"`
			Output struct {
				Format string `yaml:"format"`
			} `yaml:"output"`
			Tools struct {
				ConfirmCommands bool `yaml:"confirm_commands"`
			} `yaml:"tools"`
		}

		if err := yaml.Unmarshal(bytesOut, &parsed); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}

		if parsed.Server.Port != 9090 {
			t.Errorf("expected port 9090, got %d", parsed.Server.Port)
		}
		if parsed.Scan.MaxFileSizeKb != 2048 {
			t.Errorf("expected max_file_size_kb 2048, got %d", parsed.Scan.MaxFileSizeKb)
		}
		if parsed.Output.Format != "json" {
			t.Errorf("expected format 'json', got %q", parsed.Output.Format)
		}
		if parsed.Tools.ConfirmCommands {
			t.Errorf("expected confirm_commands false, got true")
		}

		// Verify extension list
		expectedExtensions := []string{".go", ".py", ".js", ".rs", ".diff"}
		if len(parsed.Scan.Extensions.Include) != len(expectedExtensions) {
			t.Errorf("expected extensions %v, got %v", expectedExtensions, parsed.Scan.Extensions.Include)
		}
	})

	t.Run("fails on missing critical keys", func(t *testing.T) {
		invalidYAML := `
scan:
  max_file_size_kb: 1024
`
		cfg, err := ParseConfig([]byte(invalidYAML))
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error for missing key, got nil")
		}
	})

	t.Run("fails on non-sequence key for slice override", func(t *testing.T) {
		scalarYAML := `
scan:
  extensions:
    include: ".go"
`
		cfg, err := ParseConfig([]byte(scalarYAML))
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error when target is not a sequence, got nil")
		}
	})

	t.Run("fails on intermediate non-mapping node", func(t *testing.T) {
		badYAML := `
scan: "not-a-map"
`
		cfg, err := ParseConfig([]byte(badYAML))
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error for non-mapping intermediate segment, got nil")
		}
	})

	t.Run("fails on unsupported type", func(t *testing.T) {
		cfg, err := ParseConfig([]byte(sampleValidConfig))
		if err != nil {
			t.Fatalf("unexpected parse error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"server.port": struct{}{},
		})
		if err == nil {
			t.Errorf("expected error for unsupported type, got nil")
		}
	})
}

func TestConfig_AppendExtension(t *testing.T) {
	t.Run("appends extension and enforces idempotency", func(t *testing.T) {
		cfg, err := ParseConfig([]byte(sampleValidConfig))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// First append
		if err := cfg.AppendExtension(".diff"); err != nil {
			t.Fatalf("unexpected first AppendExtension error: %v", err)
		}

		// Second append (must be idempotent - no duplicate)
		if err := cfg.AppendExtension(".diff"); err != nil {
			t.Fatalf("unexpected second AppendExtension error: %v", err)
		}

		bytesOut, err := cfg.Bytes()
		if err != nil {
			t.Fatalf("unexpected Bytes error: %v", err)
		}

		var parsed struct {
			Scan struct {
				Extensions struct {
					Include []string `yaml:"include"`
				} `yaml:"extensions"`
			} `yaml:"scan"`
		}
		if err := yaml.Unmarshal(bytesOut, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		diffCount := 0
		for _, ext := range parsed.Scan.Extensions.Include {
			if ext == ".diff" {
				diffCount++
			}
		}
		if diffCount != 1 {
			t.Errorf("expected exactly 1 instance of .diff, got %d in %v", diffCount, parsed.Scan.Extensions.Include)
		}
	})
}

func TestConfig_Write(t *testing.T) {
	t.Run("writes back to loaded file path", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected LoadConfigFile error: %v", err)
		}

		if err := cfg.AppendExtension(".diff"); err != nil {
			t.Fatalf("unexpected AppendExtension error: %v", err)
		}

		if err := cfg.Write(); err != nil {
			t.Fatalf("unexpected Write error: %v", err)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read back config: %v", err)
		}
		if !strings.Contains(string(data), ".diff") {
			t.Errorf("expected file to contain .diff, got:\n%s", string(data))
		}
	})

	t.Run("writes to DefaultConfigPath when path is empty", func(t *testing.T) {
		tempDir := t.TempDir()
		origHome := os.Getenv("HOME")
		defer os.Setenv("HOME", origHome)
		os.Setenv("HOME", tempDir)

		cmDir := filepath.Join(tempDir, ".codemender")
		if err := os.MkdirAll(cmDir, 0o755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		cfg, err := ParseConfig([]byte(sampleValidConfig))
		if err != nil {
			t.Fatalf("unexpected ParseConfig error: %v", err)
		}

		if err := cfg.Write(); err != nil {
			t.Fatalf("unexpected Write error: %v", err)
		}

		expectedPath := filepath.Join(cmDir, "config.yaml")
		if _, err := os.Stat(expectedPath); err != nil {
			t.Errorf("expected config file at %q: %v", expectedPath, err)
		}
	})
}

func TestApplyOverrides_TopLevel(t *testing.T) {
	tempDir := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tempDir)

	cmDir := filepath.Join(tempDir, ".codemender")
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	configPath := filepath.Join(cmDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := ApplyOverrides(map[string]any{
		"server.port": 9090,
	})
	if err != nil {
		t.Fatalf("unexpected ApplyOverrides error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read back config: %v", err)
	}
	if !strings.Contains(string(data), "port: 9090") {
		t.Errorf("expected file to contain port: 9090, got:\n%s", string(data))
	}
}

func TestApplyDefaultOverrides_TopLevel(t *testing.T) {
	tempDir := t.TempDir()
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", tempDir)

	cmDir := filepath.Join(tempDir, ".codemender")
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	configPath := filepath.Join(cmDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := ApplyDefaultOverrides()
	if err != nil {
		t.Fatalf("unexpected ApplyDefaultOverrides error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read back config: %v", err)
	}
	if !strings.Contains(string(data), ".rs") {
		t.Errorf("expected file to contain .rs, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), `format: "json"`) && !strings.Contains(string(data), `format: json`) {
		t.Errorf("expected file to contain format: json, got:\n%s", string(data))
	}
}

func TestDefaultConfigPath(t *testing.T) {
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	os.Setenv("HOME", "/test/home")
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("/test/home", ".codemender", "config.yaml")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}

	os.Unsetenv("HOME")
	_, err = DefaultConfigPath()
	if err == nil {
		t.Errorf("expected error when HOME unset, got nil")
	}
}
