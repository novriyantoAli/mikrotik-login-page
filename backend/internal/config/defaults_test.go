package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestEveryConfigFieldHasADefault locks the invariant that makes environment
// overrides work at all: `NewViper` calls `BindEnv` for the keys in `defaults`
// only, and `AutomaticEnv` does not look up a key Viper has never seen. A field
// without a default is therefore readable from a YAML file and silently ignored
// from the environment — which is the shape of a live bug: `WORKER_HEALTH_ADDR`
// was documented in `.env.example` and passed by compose, and the worker still
// started with the listener disabled, because compose has no config file.
//
// The list comes from the struct itself, not from `Keys()`, so it cannot agree
// with `defaults` by construction.
func TestEveryConfigFieldHasADefault(t *testing.T) {
	t.Parallel()

	missing := make([]string, 0, 4)

	var walk func(prefix string, structType reflect.Type)
	walk = func(prefix string, structType reflect.Type) {
		for index := 0; index < structType.NumField(); index++ {
			field := structType.Field(index)
			name := strings.Split(field.Tag.Get("mapstructure"), ",")[0]
			if name == "" || name == "-" {
				continue
			}

			key := name
			if prefix != "" {
				key = prefix + "." + name
			}

			// A section, not a leaf. A struct with no tagged fields inside (a
			// time.Time, say) contributes nothing, so recursing is always safe.
			if field.Type.Kind() == reflect.Struct {
				walk(key, field.Type)
				continue
			}

			if _, ok := defaults[key]; !ok {
				missing = append(missing, key)
			}
		}
	}

	walk("", reflect.TypeOf(Config{}))
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Fatalf("these keys have no default, so their environment variable is ignored: %v",
			missing)
	}
}
