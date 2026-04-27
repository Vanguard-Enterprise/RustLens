package actions

import "encoding/json"

func RedactParams(raw json.RawMessage, keys []string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	redactWalk(v, set)
	return json.Marshal(v)
}

func redactWalk(v any, keys map[string]bool) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if keys[k] {
				n[k] = "***"
				continue
			}
			redactWalk(child, keys)
		}
	case []any:
		for _, child := range n {
			redactWalk(child, keys)
		}
	}
}
