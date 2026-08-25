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
vcs:
  type: git
  commands:
    reset: "git checkout HEAD -- ."
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

func TestConfig_ApplyOverrides(t *testing.T) {
	t.Run("applies scalar and sequence overrides", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected LoadConfigFile error: %v", err)
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

		if err := cfg.Write(); err != nil {
			t.Fatalf("unexpected Write error: %v", err)
		}

		bytesOut, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
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
		tempDir := t.TempDir()
		invalidPath := filepath.Join(tempDir, "invalid.yaml")
		invalidYAML := `
scan:
  max_file_size_kb: 1024
`
		if err := os.WriteFile(invalidPath, []byte(invalidYAML), 0o600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		cfg, err := LoadConfigFile(invalidPath)
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error for missing key, got nil")
		}
	})

	t.Run("fails on non-sequence key for slice override", func(t *testing.T) {
		tempDir := t.TempDir()
		scalarPath := filepath.Join(tempDir, "scalar.yaml")
		scalarYAML := `
scan:
  extensions:
    include: ".go"
`
		if err := os.WriteFile(scalarPath, []byte(scalarYAML), 0o600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		cfg, err := LoadConfigFile(scalarPath)
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error when target is not a sequence, got nil")
		}
	})

	t.Run("fails on intermediate non-mapping node", func(t *testing.T) {
		tempDir := t.TempDir()
		badPath := filepath.Join(tempDir, "bad.yaml")
		badYAML := `
scan: "not-a-map"
`
		if err := os.WriteFile(badPath, []byte(badYAML), 0o600); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		cfg, err := LoadConfigFile(badPath)
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		err = cfg.ApplyOverrides(map[string]any{
			"scan.extensions.include": []string{".rs"},
		})
		if err == nil {
			t.Errorf("expected error for non-mapping intermediate segment, got nil")
		}
	})

	t.Run("fails on unsupported type", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
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
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
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

		if err := cfg.Write(); err != nil {
			t.Fatalf("unexpected Write error: %v", err)
		}

		bytesOut, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read back config: %v", err)
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
		configPath := filepath.Join(cmDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("unexpected LoadConfig error: %v", err)
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
			t.Errorf("expected config to contain .diff, got:\n%s", string(data))
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
	if !strings.Contains(string(data), "project_paths") || !strings.Contains(string(data), "/workspace") {
		t.Errorf("expected file to contain project_paths with /workspace, got:\n%s", string(data))
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

func TestConfig_DisableReset(t *testing.T) {
	t.Run("updates existing vcs reset command to true", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(sampleValidConfig), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected LoadConfigFile error: %v", err)
		}

		if err := cfg.DisableReset(); err != nil {
			t.Fatalf("unexpected DisableReset error: %v", err)
		}

		if err := cfg.Write(); err != nil {
			t.Fatalf("unexpected Write error: %v", err)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read back config: %v", err)
		}
		if !strings.Contains(string(data), `reset: "true"`) && !strings.Contains(string(data), `reset: true`) {
			t.Errorf("expected config to contain reset: true, got:\n%s", string(data))
		}
	})

	t.Run("fails when vcs section is missing", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "config.yaml")
		noVCS := `
scan:
  extensions:
    include:
      - ".go"
`
		if err := os.WriteFile(configPath, []byte(noVCS), 0o600); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		cfg, err := LoadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected LoadConfigFile error: %v", err)
		}

		if err := cfg.DisableReset(); err == nil {
			t.Errorf("expected error when vcs section is missing, got nil")
		}
	})
}
