package fallback

import "encoding/json"

// jsonUnmarshal exists so Custom.UnmarshalJSON does not import encoding/json
// under a name the generated file might also want.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
