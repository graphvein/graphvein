package gorm

import "strings"

// parseTagSetting ports schema.ParseTagSetting (gorm.io/gorm v1.31.2
// schema/utils.go:16-43): settings split at sep (a backslash before sep
// joins two parts), keys upper-cased and trimmed, a key without a value
// mapped to itself, `\"` unescaped in values.
func parseTagSetting(str, sep string) map[string]string {
	settings := map[string]string{}
	names := strings.Split(str, sep)
	var parsed []string
	for i := 0; i < len(names); i++ {
		s := names[i]
		for strings.HasSuffix(s, `\`) && i+1 < len(names) {
			i++
			s = s[:len(s)-1] + sep + names[i]
		}
		parsed = append(parsed, s)
	}
	for _, tag := range parsed {
		values := strings.Split(tag, ":")
		k := strings.TrimSpace(strings.ToUpper(values[0]))
		if len(values) >= 2 {
			settings[k] = strings.ReplaceAll(strings.Join(values[1:], ":"), `\"`, `"`)
		} else if k != "" {
			settings[k] = k
		}
	}
	return settings
}

// checkTruth ports utils.CheckTruth: a value is true when it is not empty
// and not "false" (any case).
func checkTruth(vals ...string) bool {
	for _, v := range vals {
		if v != "" && !strings.EqualFold(v, "false") {
			return true
		}
	}
	return false
}
