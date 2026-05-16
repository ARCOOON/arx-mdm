package policy

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type pathContribution struct {
	Source ProfileSource
	Value  any
}

// MergeAssignedPayloads merges multiple declarative profile payloads using restrictive-wins semantics.
func MergeAssignedPayloads(inputs []AssignedPayload) (*MergeResult, error) {
	out := &MergeResult{
		EffectivePayload: map[string]any{},
		FlatSettings:     nil,
		ConflictPaths:    map[string]struct{}{},
		ConflictProfiles: map[uuid.UUID]struct{}{},
	}
	if len(inputs) == 0 {
		out.FlatSettings = []EffectiveSetting{}
		return out, nil
	}

	flatContributors := map[string][]pathContribution{}
	for _, in := range inputs {
		if in.Source.ID == uuid.Nil {
			continue
		}
		root := map[string]any{}
		if len(in.Payload) > 0 && string(in.Payload) != "null" {
			if err := json.Unmarshal(in.Payload, &root); err != nil {
				return nil, fmt.Errorf("profile %s payload: %w", in.Source.ID, err)
			}
		}
		flattenContributions("", root, in.Source, flatContributors)
	}

	mergedLeaves := map[string]any{}
	var conflicts []SettingConflict

	paths := sortedKeys(flatContributors)
	for _, path := range paths {
		parts := flatContributors[path]
		leafKey := pathLeaf(path)
		val, conflict, profilesInvolved, norms := mergeContributions(path, leafKey, parts)
		mergedLeaves[path] = val

		srcProfiles := make([]ConflictProfile, 0, len(parts))
		seen := map[uuid.UUID]struct{}{}
		for _, p := range parts {
			if _, ok := seen[p.Source.ID]; ok {
				continue
			}
			seen[p.Source.ID] = struct{}{}
			srcProfiles = append(srcProfiles, ConflictProfile{ID: p.Source.ID, Name: p.Source.Name})
		}
		sort.Slice(srcProfiles, func(i, j int) bool {
			return strings.ToLower(srcProfiles[i].Name) < strings.ToLower(srcProfiles[j].Name)
		})

		out.FlatSettings = append(out.FlatSettings, EffectiveSetting{
			Path:           path,
			Value:          val,
			Conflict:       conflict,
			SourceProfiles: srcProfiles,
		})

		if conflict {
			out.ConflictPaths[path] = struct{}{}
			for _, cp := range profilesInvolved {
				out.ConflictProfiles[cp.ID] = struct{}{}
			}
			conflicts = append(conflicts, SettingConflict{
				Path:                  path,
				EffectiveValue:        val,
				ConflictingProfiles:   profilesInvolved,
				ContributedNormalized: norms,
			})
		}
	}

	sort.Slice(out.FlatSettings, func(i, j int) bool {
		return out.FlatSettings[i].Path < out.FlatSettings[j].Path
	})

	out.Conflicts = conflicts
	out.EffectivePayload = unflattenLeaves(mergedLeaves)
	return out, nil
}

func flattenContributions(prefix string, v any, src ProfileSource, acc map[string][]pathContribution) {
	switch typed := v.(type) {
	case map[string]any:
		if len(typed) == 0 && prefix != "" {
			acc[prefix] = append(acc[prefix], pathContribution{Source: src, Value: typed})
			return
		}
		keys := sortedKeysMap(typed)
		for _, k := range keys {
			child := typed[k]
			nextPrefix := k
			if prefix != "" {
				nextPrefix = prefix + "." + k
			}
			flattenContributions(nextPrefix, child, src, acc)
		}
	case []any:
		if prefix != "" {
			acc[prefix] = append(acc[prefix], pathContribution{Source: src, Value: typed})
		}
	default:
		if prefix != "" {
			acc[prefix] = append(acc[prefix], pathContribution{Source: src, Value: typed})
		}
	}
}

func mergeContributions(fullPath, leafKey string, parts []pathContribution) (effective any, conflict bool, conflictingProfiles []ConflictProfile, norms []any) {
	if len(parts) == 0 {
		return nil, false, nil, nil
	}
	if len(parts) == 1 {
		v := normalizeJSON(parts[0].Value)
		return v, false, nil, []any{v}
	}

	firstKind := contributionKind(parts[0].Value)
	allSameKind := true
	for _, p := range parts[1:] {
		if contributionKind(p.Value) != firstKind {
			allSameKind = false
			break
		}
	}
	if !allSameKind {
		cps := conflictParticipants(parts)
		nv := contributedNormalizedSlice(parts)
		return restrictivePickMixed(leafKey, parts), true, cps, nv
	}

	switch firstKind {
	case kindBool:
		vals := make([]bool, 0, len(parts))
		for _, p := range parts {
			b, ok := jsonScalarBool(p.Value)
			if !ok {
				cps := conflictParticipants(parts)
				return restrictivePickMixed(leafKey, parts), true, cps, contributedNormalizedSlice(parts)
			}
			vals = append(vals, b)
		}
		if uniqueBools(vals) > 1 {
			effective = mergeBoolRestrictive(leafKey, vals)
			return effective, true, conflictParticipants(parts), boolSliceToAny(vals)
		}
		return mergeBoolRestrictive(leafKey, vals), false, nil, boolSliceToAny(vals)

	case kindNumber:
		vals := make([]float64, 0, len(parts))
		for _, p := range parts {
			f, ok := jsonScalarFloat(p.Value)
			if !ok {
				cps := conflictParticipants(parts)
				return restrictivePickMixed(leafKey, parts), true, cps, contributedNormalizedSlice(parts)
			}
			vals = append(vals, f)
		}
		if uniqueFloats(vals) > 1 {
			effective = mergeNumericRestrictive(leafKey, vals)
			return effective, true, conflictParticipants(parts), floatSliceToAny(vals)
		}
		return mergeNumericRestrictive(leafKey, vals), false, nil, floatSliceToAny(vals)

	case kindString:
		strs := make([]string, 0, len(parts))
		for _, p := range parts {
			s, ok := p.Value.(string)
			if !ok {
				cps := conflictParticipants(parts)
				return restrictivePickMixed(leafKey, parts), true, cps, contributedNormalizedSlice(parts)
			}
			strs = append(strs, s)
		}
		if uniqueStrings(strs) > 1 {
			effective = mergeStringRestrictive(strs)
			return effective, true, conflictParticipants(parts), stringSliceToAny(strs)
		}
		return strs[0], false, nil, stringSliceToAny(strs)

	case kindArray:
		effective, conflictArr := mergeArrayRestrictive(parts)
		if conflictArr {
			return effective, true, conflictParticipants(parts), contributedNormalizedSlice(parts)
		}
		return effective, false, nil, contributedNormalizedSlice(parts)

	case kindObject:
		objs := make([]map[string]any, 0, len(parts))
		for _, p := range parts {
			m, ok := p.Value.(map[string]any)
			if !ok {
				cps := conflictParticipants(parts)
				return restrictivePickMixed(leafKey, parts), true, cps, contributedNormalizedSlice(parts)
			}
			objs = append(objs, m)
		}
		if uniqueMaps(objs) > 1 {
			merged := mergeObjectMapsRestrictive(objs)
			return merged, true, conflictParticipants(parts), contributedNormalizedSlice(parts)
		}
		return objs[0], false, nil, contributedNormalizedSlice(parts)

	default:
		cps := conflictParticipants(parts)
		nv := contributedNormalizedSlice(parts)
		return restrictivePickMixed(leafKey, parts), uniqueNormalizedValues(nv) > 1, cps, nv
	}
}

type contribKind int

const (
	kindUnknown contribKind = iota
	kindBool
	kindNumber
	kindString
	kindArray
	kindObject
)

func contributionKind(v any) contribKind {
	switch v.(type) {
	case bool:
		return kindBool
	case float64:
		return kindNumber
	case json.Number:
		return kindNumber
	case string:
		return kindString
	case []any:
		return kindArray
	case map[string]any:
		return kindObject
	default:
		return kindUnknown
	}
}

func jsonScalarBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	default:
		return false, false
	}
}

func jsonScalarFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func mergeBoolRestrictive(leafKey string, vals []bool) bool {
	key := strings.ToLower(leafKey)
	switch {
	case strings.HasSuffix(key, "_disabled"),
		strings.HasSuffix(key, "_blocked"),
		strings.HasSuffix(key, "_denied"):
		out := false
		for _, v := range vals {
			if v {
				out = true
			}
		}
		return out
	default:
		out := true
		for _, v := range vals {
			if !v {
				out = false
			}
		}
		return out
	}
}

func mergeNumericRestrictive(leafKey string, vals []float64) float64 {
	key := strings.ToLower(leafKey)
	switch {
	case strings.Contains(key, "min_"):
		max := vals[0]
		for _, v := range vals[1:] {
			if v > max {
				max = v
			}
		}
		return max
	default:
		min := vals[0]
		for _, v := range vals[1:] {
			if v < min {
				min = v
			}
		}
		return min
	}
}

func mergeStringRestrictive(vals []string) string {
	sort.Strings(vals)
	return vals[0]
}

func mergeArrayRestrictive(parts []pathContribution) (any, bool) {
	allStrings := true
	var union []string
	seen := map[string]struct{}{}

	firstNormalized := normalizeJSON(parts[0].Value)
	conflict := false

	for _, p := range parts {
		arr, ok := p.Value.([]any)
		if !ok {
			return restrictivePickMixed("", parts), true
		}
		norm := normalizeJSON(arr)
		if !reflect.DeepEqual(norm, firstNormalized) {
			conflict = true
		}
		for _, el := range arr {
			s, ok := el.(string)
			if !ok {
				allStrings = false
				break
			}
			if _, dup := seen[s]; !dup {
				seen[s] = struct{}{}
				union = append(union, s)
			}
		}
		if !allStrings {
			break
		}
	}

	if allStrings && conflict {
		sort.Strings(union)
		out := make([]any, 0, len(union))
		for _, s := range union {
			out = append(out, s)
		}
		return out, true
	}
	if !conflict {
		return firstNormalized, false
	}

	norms := contributedNormalizedSlice(parts)
	picked := norms[0]
	for _, cand := range norms[1:] {
		picked = restrictivePickScalars("", picked, cand)
	}
	return picked, true
}

func mergeObjectMapsRestrictive(objs []map[string]any) map[string]any {
	out := map[string]any{}
	for _, o := range objs {
		for k, v := range o {
			key := strings.ToLower(strings.TrimSpace(k))
			if _, exists := out[k]; !exists {
				out[k] = v
				continue
			}
			out[k] = restrictivePickScalars(key, out[k], v)
		}
	}
	return out
}

func restrictivePickMixed(leafKey string, parts []pathContribution) any {
	nv := contributedNormalizedSlice(parts)
	if len(nv) == 0 {
		return nil
	}
	out := nv[0]
	for _, cand := range nv[1:] {
		out = restrictivePickScalars(leafKey, out, cand)
	}
	return out
}

func restrictivePickScalars(leafKey string, a, b any) any {
	ka := contributionKind(a)
	kb := contributionKind(b)
	if ka != kb {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) <= string(jb) {
			return a
		}
		return b
	}
	switch ka {
	case kindBool:
		ba, _ := jsonScalarBool(a)
		bb, _ := jsonScalarBool(b)
		return mergeBoolRestrictive(leafKey, []bool{ba, bb})
	case kindNumber:
		fa, _ := jsonScalarFloat(a)
		fb, _ := jsonScalarFloat(b)
		return mergeNumericRestrictive(leafKey, []float64{fa, fb})
	case kindString:
		sa, _ := a.(string)
		sb, _ := b.(string)
		return mergeStringRestrictive([]string{sa, sb})
	default:
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) <= string(jb) {
			return a
		}
		return b
	}
}

func normalizeJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func contributedNormalizedSlice(parts []pathContribution) []any {
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		out = append(out, normalizeJSON(p.Value))
	}
	return out
}

func uniqueNormalizedValues(vals []any) int {
	if len(vals) <= 1 {
		return len(vals)
	}
	seen := []any{}
outer:
	for _, v := range vals {
		for _, e := range seen {
			if reflect.DeepEqual(v, e) {
				continue outer
			}
		}
		seen = append(seen, v)
	}
	return len(seen)
}

func conflictParticipants(parts []pathContribution) []ConflictProfile {
	out := []ConflictProfile{}
	seen := map[uuid.UUID]struct{}{}
	for _, p := range parts {
		if p.Source.ID == uuid.Nil {
			continue
		}
		if _, ok := seen[p.Source.ID]; ok {
			continue
		}
		seen[p.Source.ID] = struct{}{}
		out = append(out, ConflictProfile{ID: p.Source.ID, Name: p.Source.Name})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func uniqueBools(vals []bool) int {
	t, f := false, false
	for _, v := range vals {
		if v {
			t = true
		} else {
			f = true
		}
	}
	n := 0
	if t {
		n++
	}
	if f {
		n++
	}
	return n
}

func uniqueFloats(vals []float64) int {
	uniq := []float64{}
outer:
	for _, v := range vals {
		for _, e := range uniq {
			if nearlyEqual(v, e) {
				continue outer
			}
		}
		uniq = append(uniq, v)
	}
	return len(uniq)
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

func uniqueStrings(vals []string) int {
	m := map[string]struct{}{}
	for _, s := range vals {
		m[s] = struct{}{}
	}
	return len(m)
}

func uniqueMaps(vals []map[string]any) int {
	norms := [][]byte{}
outer:
	for _, v := range vals {
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		for _, ex := range norms {
			if string(ex) == string(b) {
				continue outer
			}
		}
		norms = append(norms, b)
	}
	return len(norms)
}

func boolSliceToAny(v []bool) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = v[i]
	}
	return out
}

func floatSliceToAny(v []float64) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = v[i]
	}
	return out
}

func stringSliceToAny(v []string) []any {
	out := make([]any, len(v))
	for i := range v {
		out[i] = v[i]
	}
	return out
}

func pathLeaf(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

func sortedKeys(m map[string][]pathContribution) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unflattenLeaves(leaves map[string]any) map[string]any {
	root := map[string]any{}
	paths := sortedKeysLeaves(leaves)
	for _, p := range paths {
		setAtDottedPath(root, p, leaves[p])
	}
	return root
}

func sortedKeysLeaves(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setAtDottedPath(root map[string]any, path string, val any) {
	if path == "" {
		return
	}
	segs := strings.Split(path, ".")
	cur := root
	for i := 0; i < len(segs)-1; i++ {
		sg := segs[i]
		next, ok := cur[sg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[sg] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = val
}
