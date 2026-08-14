package main

import (
	"strings"
)

// HasFormatFlag inspects a list of CLI flags to check if an explicit format
// flag (--format, --format=..., -f, -f=...) has been specified.
func HasFormatFlag(flags []string) bool {
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		if flag == "--format" || flag == "-f" || flag == "--help" || flag == "-h" {
			return true
		}
		if strings.HasPrefix(flag, "--format=") || strings.HasPrefix(flag, "-f=") {
			return true
		}
	}
	return false
}

// InjectFormatFlags ensures that "--format json" is included in the flags
// unless an explicit format flag is already present.
func InjectFormatFlags(flags []string) []string {
	if HasFormatFlag(flags) {
		result := make([]string, len(flags))
		copy(result, flags)
		return result
	}

	result := make([]string, len(flags), len(flags)+2)
	copy(result, flags)
	return append(result, "--format", "json")
}

// ConfigureEnvironment updates or appends headless non-interactive defaults
// (NO_COLOR=1 and TERM=dumb) while preserving all existing environment variables
// (e.g. auth tokens and system PATH).
func ConfigureEnvironment(baseEnv []string) []string {
	envMap := make(map[string]string)
	orderedKeys := make([]string, 0, len(baseEnv)+2)

	for _, env := range baseEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			k, v := parts[0], parts[1]
			if _, exists := envMap[k]; !exists {
				orderedKeys = append(orderedKeys, k)
			}
			envMap[k] = v
		}
	}

	// Set required defaults
	if _, exists := envMap["NO_COLOR"]; !exists {
		orderedKeys = append(orderedKeys, "NO_COLOR")
	}
	envMap["NO_COLOR"] = "1"

	if _, exists := envMap["TERM"]; !exists {
		orderedKeys = append(orderedKeys, "TERM")
	}
	envMap["TERM"] = "dumb"

	result := make([]string, 0, len(orderedKeys))
	for _, k := range orderedKeys {
		result = append(result, k+"="+envMap[k])
	}

	return result
}
