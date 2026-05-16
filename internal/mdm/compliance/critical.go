package compliance

import (
	"strings"
)

// PayloadCarriesCriticalSecurityControls reports whether merged declarative policy assigns security controls
// whose enforcement failures must mark the device non-compliant.
func PayloadCarriesCriticalSecurityControls(root map[string]any) bool {
	if root == nil || len(root) == 0 {
		return false
	}
	if v, ok := root["security"]; ok && !isEffectivelyEmpty(v) {
		return true
	}
	if v, ok := root["registry"]; ok && !isEffectivelyEmpty(v) {
		return true
	}
	if v, ok := root["windows_firewall"]; ok && !isEffectivelyEmpty(v) {
		return true
	}
	if v, ok := root["firewall"]; ok && !isEffectivelyEmpty(v) {
		return true
	}
	if v, ok := root["wifi"]; ok && !isEffectivelyEmpty(v) {
		return true
	}
	if s, ok := root["wifi_profile_xml"].(string); ok && strings.TrimSpace(s) != "" {
		return true
	}
	if win, ok := root["windows"].(map[string]any); ok {
		for _, k := range []string{"registry", "windows_firewall", "firewall", "wifi", "wifi_profile_xml"} {
			if v, ok := win[k]; ok && !isEffectivelyEmpty(v) {
				return true
			}
		}
	}
	if lx, ok := root["linux"].(map[string]any); ok {
		for _, k := range []string{"sysctl", "linux_firewall", "firewall"} {
			if v, ok := lx[k]; ok && !isEffectivelyEmpty(v) {
				return true
			}
		}
	}
	if union, ok := root["linux_firewall"].(map[string]any); ok && !isEffectivelyEmpty(union) {
		return true
	}
	if ad, ok := root["android"].(map[string]any); ok && !isEffectivelyEmpty(ad) {
		return true
	}
	return false
}

func isEffectivelyEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	case string:
		return strings.TrimSpace(t) == ""
	default:
		return false
	}
}
