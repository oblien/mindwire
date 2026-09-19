package agent

import (
	"fmt"
	"strconv"
	"strings"
)

// NativeSettingsModule reports declared non-secret defaults from the harness's
// own config. Managed overrides take precedence; an empty override resets to
// the native default. The client never needs to parse harness config files.
type NativeSettingsModule interface {
	NativeSettings(CredStore) map[string]string
}

func ReadSettings(adapter Adapter, store CredStore) map[string]string {
	allow := SettingsKeys(adapter.Settings())
	out := map[string]string{}
	if native, ok := adapter.(NativeSettingsModule); ok {
		for key, value := range native.NativeSettings(store) {
			if allow[key] {
				out[key] = value
			}
		}
	}
	for key, value := range store.All() {
		if !allow[key] {
			continue
		}
		if value != "" || out[key] == "" {
			out[key] = value
		}
	}
	return out
}

// NormalizeSettings validates the complete patch before anything is persisted.
// It retains raw/canonical compatibility and the existing unknown-key filtering.
func NormalizeSettings(schema SettingsSchema, input map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range input {
		raw, ok := ResolveSettingKey(schema, key)
		if !ok {
			continue
		}
		if direct, exists := input[raw]; exists {
			value = direct
		}
		out[raw] = value
	}
	for _, section := range schema.Sections {
		for _, field := range section.Fields {
			value, provided := out[field.Key]
			if !provided || value == "" {
				continue
			}
			if field.Type == FieldSelect && len(field.Options) > 0 {
				valid := false
				for _, option := range field.Options {
					valid = valid || value == option.Value
				}
				if !valid {
					return nil, fmt.Errorf("choose a supported value for %s", field.Label)
				}
			}
			if field.Type == FieldToggle && value != "true" && value != "false" {
				return nil, fmt.Errorf("%s must be true or false", field.Label)
			}
			if field.InputMode == "numeric" {
				number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				if err != nil || (field.Minimum != nil && number < *field.Minimum) || (field.Maximum != nil && number > *field.Maximum) {
					return nil, fmt.Errorf("enter a valid whole number for %s", field.Label)
				}
				out[field.Key] = strconv.FormatInt(number, 10)
			}
		}
	}
	return out, nil
}
