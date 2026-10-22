package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up from this test's directory until it finds the repository
// root (the directory holding both backend/ and contracts/).
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if fileExists(filepath.Join(dir, "contracts", "health.response.json")) &&
			fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found above %s", dir)
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// loadContract reads one shared contract fixture from contracts/.
func loadContract(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join(repoRoot(t), "contracts", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract fixture %s: %v", path, err)
	}
	return raw
}

func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode json %q: %v", string(raw), err)
	}
	return decoded
}

// assertSameJSONShape fails when two JSON values have different keys or value
// kinds. It deliberately ignores the values themselves: the fixture's latency
// is an example, but its shape is the contract.
func assertSameJSONShape(t *testing.T, expected, actual any, path string) {
	t.Helper()

	if diffs := jsonShapeDiffs(path, expected, actual); len(diffs) > 0 {
		t.Fatalf("json shape mismatch:\n  %s", strings.Join(diffs, "\n  "))
	}
}

func jsonShapeDiffs(path string, expected, actual any) []string {
	switch expectedTyped := expected.(type) {
	case map[string]any:
		actualTyped, ok := actual.(map[string]any)
		if !ok {
			return []string{path + ": expected object, got " + jsonKind(actual)}
		}

		var diffs []string
		for key := range expectedTyped {
			if _, ok := actualTyped[key]; !ok {
				diffs = append(diffs, path+"."+key+": missing")
			}
		}
		for key := range actualTyped {
			if _, ok := expectedTyped[key]; !ok {
				diffs = append(diffs, path+"."+key+": unexpected")
			}
		}
		for key, value := range expectedTyped {
			if other, ok := actualTyped[key]; ok {
				diffs = append(diffs, jsonShapeDiffs(path+"."+key, value, other)...)
			}
		}
		return diffs

	case []any:
		actualTyped, ok := actual.([]any)
		if !ok {
			return []string{path + ": expected array, got " + jsonKind(actual)}
		}
		if len(actualTyped) != len(expectedTyped) {
			return []string{fmt.Sprintf("%s: expected array of length %d, got %d", path, len(expectedTyped), len(actualTyped))}
		}
		var diffs []string
		for index := range expectedTyped {
			diffs = append(diffs, jsonShapeDiffs(fmt.Sprintf("%s[%d]", path, index), expectedTyped[index], actualTyped[index])...)
		}
		return diffs

	default:
		if jsonKind(expected) != jsonKind(actual) {
			return []string{path + ": expected " + jsonKind(expected) + ", got " + jsonKind(actual)}
		}
		return nil
	}
}

func jsonKind(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case json.Number, float64:
		return "number"
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// assertSameKeys checks that two JSON objects expose exactly the same keys.
func assertSameKeys(t *testing.T, expected, actual map[string]any, path string) {
	t.Helper()

	var diffs []string
	for key := range expected {
		if _, ok := actual[key]; !ok {
			diffs = append(diffs, path+"."+key+": missing")
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			diffs = append(diffs, path+"."+key+": unexpected")
		}
	}

	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Fatalf("json keys mismatch:\n  %s", strings.Join(diffs, "\n  "))
	}
}
