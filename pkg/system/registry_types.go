package system

// RegistryValue is a single registry value read from the Windows Registry API.
type RegistryValue struct {
	ValueName string `json:"value_name"`
	Type      string `json:"type"` // string, expand_string, dword, qword, binary, multi_string
	Data      string `json:"data"` // UTF-8 text for string types; hex for binary
}

// RegistryWriteInput describes a write to a named value under a key path.
type RegistryWriteInput struct {
	KeyPath   string `json:"key_path"`
	ValueName string `json:"value_name"`
	Type      string `json:"type"` // string, expand_string, dword, qword, binary (hex), multi_string (lines joined with \0 in JSON as \n separated)
	Data      string `json:"data"`
}
