//go:build windows

package system

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func parseKeyPath(full string) (registry.Key, string, error) {
	full = strings.TrimSpace(full)
	if full == "" {
		return 0, "", fmt.Errorf("system: empty registry path")
	}
	parts := strings.SplitN(full, `\`, 2)
	rootName := strings.ToUpper(strings.TrimSpace(parts[0]))
	var root registry.Key
	switch rootName {
	case "HKLM", "HKEY_LOCAL_MACHINE":
		root = registry.LOCAL_MACHINE
	case "HKCU", "HKEY_CURRENT_USER":
		root = registry.CURRENT_USER
	case "HKCR", "HKEY_CLASSES_ROOT":
		root = registry.CLASSES_ROOT
	case "HKU", "HKEY_USERS":
		root = registry.USERS
	case "HKCC", "HKEY_CURRENT_CONFIG":
		root = registry.CURRENT_CONFIG
	case "HKPD", "HKEY_PERFORMANCE_DATA":
		root = registry.PERFORMANCE_DATA
	default:
		return 0, "", fmt.Errorf("system: unknown registry root %q", parts[0])
	}
	if len(parts) == 1 {
		return root, "", nil
	}
	sub := strings.TrimSpace(parts[1])
	sub = strings.Trim(sub, `\`)
	return root, sub, nil
}

func openPath(keyPath string, writable bool) (registry.Key, error) {
	root, sub, err := parseKeyPath(keyPath)
	if err != nil {
		return 0, err
	}
	access := uint32(registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS)
	if writable {
		access |= registry.SET_VALUE | registry.WRITE | registry.CREATE_SUB_KEY
	}
	if sub == "" {
		return registry.OpenKey(root, "", access)
	}
	return registry.OpenKey(root, sub, access)
}

func typeName(t uint32) string {
	switch t {
	case registry.SZ:
		return "string"
	case registry.EXPAND_SZ:
		return "expand_string"
	case registry.DWORD, registry.DWORD_BIG_ENDIAN:
		return "dword"
	case registry.QWORD:
		return "qword"
	case registry.BINARY:
		return "binary"
	case registry.MULTI_SZ:
		return "multi_string"
	default:
		return fmt.Sprintf("unknown_%d", t)
	}
}

// RegistryRead returns the value named valueName under keyPath (valueName may be empty for the default value).
func RegistryRead(keyPath, valueName string) (RegistryValue, error) {
	k, err := openPath(keyPath, false)
	if err != nil {
		return RegistryValue{}, err
	}
	defer k.Close()

	n, valType, err := k.GetValue(valueName, nil)
	if err != nil {
		return RegistryValue{}, err
	}
	buf := make([]byte, n)
	n, valType, err = k.GetValue(valueName, buf)
	if err != nil {
		return RegistryValue{}, err
	}
	val := buf[:n]
	out := RegistryValue{ValueName: valueName, Type: typeName(valType)}
	switch valType {
	case registry.SZ, registry.EXPAND_SZ:
		s, _, err := k.GetStringValue(valueName)
		if err != nil {
			return RegistryValue{}, err
		}
		out.Data = s
	case registry.DWORD, registry.DWORD_BIG_ENDIAN:
		n, _, err := k.GetIntegerValue(valueName)
		if err != nil {
			return RegistryValue{}, err
		}
		out.Data = strconv.FormatUint(uint64(n), 10)
	case registry.QWORD:
		n, _, err := k.GetIntegerValue(valueName)
		if err != nil {
			return RegistryValue{}, err
		}
		out.Data = strconv.FormatUint(uint64(n), 10)
	case registry.MULTI_SZ:
		ss, _, err := k.GetStringsValue(valueName)
		if err != nil {
			return RegistryValue{}, err
		}
		out.Data = strings.Join(ss, "\n")
	case registry.BINARY:
		out.Data = hex.EncodeToString(val)
	default:
		out.Data = hex.EncodeToString(val)
	}
	return out, nil
}

func parseWriteType(s string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "string", "sz", "reg_sz":
		return registry.SZ, nil
	case "expand_string", "expand_sz", "reg_expand_sz":
		return registry.EXPAND_SZ, nil
	case "dword", "reg_dword":
		return registry.DWORD, nil
	case "qword", "reg_qword":
		return registry.QWORD, nil
	case "binary", "reg_binary":
		return registry.BINARY, nil
	case "multi_string", "multi_sz", "reg_multi_sz":
		return registry.MULTI_SZ, nil
	default:
		return 0, fmt.Errorf("system: unsupported registry value type %q", s)
	}
}

// RegistryWrite creates or updates a value under keyPath.
func RegistryWrite(in RegistryWriteInput) error {
	if strings.TrimSpace(in.KeyPath) == "" {
		return fmt.Errorf("system: key_path is required")
	}
	root, sub, err := parseKeyPath(in.KeyPath)
	if err != nil {
		return err
	}
	typ, err := parseWriteType(in.Type)
	if err != nil {
		return err
	}
	// Ensure key exists (create missing segments).
	k, _, err := registry.CreateKey(root, sub, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer k.Close()

	switch typ {
	case registry.SZ, registry.EXPAND_SZ:
		return k.SetStringValue(in.ValueName, in.Data)
	case registry.DWORD:
		n, err := strconv.ParseUint(strings.TrimSpace(in.Data), 0, 32)
		if err != nil {
			return fmt.Errorf("system: dword parse: %w", err)
		}
		return k.SetDWordValue(in.ValueName, uint32(n))
	case registry.QWORD:
		n, err := strconv.ParseUint(strings.TrimSpace(in.Data), 0, 64)
		if err != nil {
			return fmt.Errorf("system: qword parse: %w", err)
		}
		return k.SetQWordValue(in.ValueName, n)
	case registry.MULTI_SZ:
		lines := strings.Split(in.Data, "\n")
		return k.SetStringsValue(in.ValueName, lines)
	case registry.BINARY:
		b, err := hex.DecodeString(strings.TrimSpace(strings.ReplaceAll(in.Data, " ", "")))
		if err != nil {
			return fmt.Errorf("system: binary hex decode: %w", err)
		}
		return k.SetBinaryValue(in.ValueName, b)
	default:
		return fmt.Errorf("system: unsupported write type %d", typ)
	}
}

// RegistryDelete removes a named value when valueName is non-empty.
// If deleteKey is true and valueName is empty, deletes the subkey named by keyPath (must have no subkeys).
func RegistryDelete(keyPath, valueName string, deleteKey bool) error {
	if deleteKey {
		if strings.TrimSpace(valueName) != "" {
			return fmt.Errorf("system: delete_key requires empty value_name")
		}
		root, sub, err := parseKeyPath(keyPath)
		if err != nil {
			return err
		}
		if sub == "" {
			return fmt.Errorf("system: refusing to delete registry root")
		}
		i := strings.LastIndex(sub, `\`)
		if i < 0 {
			return registry.DeleteKey(root, sub)
		}
		parent := sub[:i]
		leaf := sub[i+1:]
		pk, err := registry.OpenKey(root, parent, registry.SET_VALUE|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			return err
		}
		defer pk.Close()
		return registry.DeleteKey(pk, leaf)
	}
	if strings.TrimSpace(valueName) == "" {
		return fmt.Errorf("system: value_name is required unless delete_key is true")
	}
	k, err := openPath(keyPath, true)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(valueName)
}
