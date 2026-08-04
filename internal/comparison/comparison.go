package comparison

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/rkthtrifork/gitops-local-render/internal/engine"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

type Report struct {
	Added          []ObjectChange `json:"added"`
	Removed        []ObjectChange `json:"removed"`
	Changed        []ObjectChange `json:"changed"`
	SkippedAdded   []string       `json:"skippedAdded,omitempty"`
	SkippedRemoved []string       `json:"skippedRemoved,omitempty"`
}

type ObjectChange struct {
	Object      ObjectIdentity `json:"object"`
	BaseUnit    string         `json:"baseUnit,omitempty"`
	HeadUnit    string         `json:"headUnit,omitempty"`
	BaseSources []string       `json:"baseSources,omitempty"`
	HeadSources []string       `json:"headSources,omitempty"`
	Fields      []FieldChange  `json:"fields,omitempty"`
}

type ObjectIdentity struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence,omitempty"`
}

type FieldChange struct {
	Path          string `json:"path"`
	Before        any    `json:"before"`
	After         any    `json:"after"`
	BeforePresent bool   `json:"beforePresent"`
	AfterPresent  bool   `json:"afterPresent"`
	Redacted      bool   `json:"redacted,omitempty"`
}

func Compare(base, head *engine.Result) Report {
	baseObjects := index(base.Records)
	headObjects := index(head.Records)
	keys := unionKeys(baseObjects, headObjects)
	report := Report{
		Added:   []ObjectChange{},
		Removed: []ObjectChange{},
		Changed: []ObjectChange{},
	}

	for _, key := range keys {
		baseRecord, inBase := baseObjects[key]
		headRecord, inHead := headObjects[key]
		identity := key.Identity
		if key.Occurrence > 0 {
			identity.Occurrence = key.Occurrence + 1
		}
		switch {
		case !inBase:
			report.Added = append(report.Added, ObjectChange{Object: identity, HeadUnit: headRecord.Unit, HeadSources: headRecord.Sources})
		case !inHead:
			report.Removed = append(report.Removed, ObjectChange{Object: identity, BaseUnit: baseRecord.Unit, BaseSources: baseRecord.Sources})
		default:
			fields := diffObject(baseRecord.Object, headRecord.Object)
			if len(fields) > 0 || baseRecord.Unit != headRecord.Unit || !slices.Equal(baseRecord.Sources, headRecord.Sources) {
				report.Changed = append(report.Changed, ObjectChange{
					Object: identity, BaseUnit: baseRecord.Unit, HeadUnit: headRecord.Unit,
					BaseSources: baseRecord.Sources, HeadSources: headRecord.Sources, Fields: fields,
				})
			}
		}
	}

	report.SkippedAdded, report.SkippedRemoved = compareStrings(base.Skipped, head.Skipped)
	return report
}

func (r Report) Different() bool {
	return len(r.Added) > 0 || len(r.Removed) > 0 || len(r.Changed) > 0 || len(r.SkippedAdded) > 0 || len(r.SkippedRemoved) > 0
}

func WriteSummary(output io.Writer, report Report) error {
	for _, change := range report.Added {
		if _, err := fmt.Fprintf(output, "Added: %s", change.Object); err != nil {
			return err
		}
		if change.HeadUnit != "" {
			if _, err := fmt.Fprintf(output, " (from %s)", change.HeadUnit); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}
	for _, change := range report.Removed {
		if _, err := fmt.Fprintf(output, "Removed: %s", change.Object); err != nil {
			return err
		}
		if change.BaseUnit != "" {
			if _, err := fmt.Fprintf(output, " (from %s)", change.BaseUnit); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}
	for _, change := range report.Changed {
		if _, err := fmt.Fprintf(output, "Changed: %s\n", change.Object); err != nil {
			return err
		}
		if change.BaseUnit != change.HeadUnit {
			if _, err := fmt.Fprintf(output, "  provenance: %s -> %s\n", change.BaseUnit, change.HeadUnit); err != nil {
				return err
			}
		}
		if !slices.Equal(change.BaseSources, change.HeadSources) {
			if _, err := fmt.Fprintf(output, "  sources: %s -> %s\n", strings.Join(change.BaseSources, ","), strings.Join(change.HeadSources, ",")); err != nil {
				return err
			}
		}
		for _, field := range change.Fields {
			if field.Redacted {
				if _, err := fmt.Fprintf(output, "  %s: %s -> %s\n", field.Path, formatRedactedValue(field.BeforePresent), formatRedactedValue(field.AfterPresent)); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(output, "  %s: %s -> %s\n", field.Path, formatFieldValue(field.Before, field.BeforePresent), formatFieldValue(field.After, field.AfterPresent)); err != nil {
				return err
			}
		}
	}
	for _, unit := range report.SkippedAdded {
		if _, err := fmt.Fprintf(output, "Skipped only in head: %s\n", unit); err != nil {
			return err
		}
	}
	for _, unit := range report.SkippedRemoved {
		if _, err := fmt.Fprintf(output, "Skipped only in base: %s\n", unit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "Summary: %d added, %d removed, %d changed\n", len(report.Added), len(report.Removed), len(report.Changed))
	return err
}

func WriteJSON(output io.Writer, report Report) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func (i ObjectIdentity) String() string {
	namespace := i.Namespace
	if namespace == "" {
		namespace = "<cluster>"
	}
	occurrence := ""
	if i.Occurrence > 0 {
		occurrence = fmt.Sprintf("#%d", i.Occurrence)
	}
	return fmt.Sprintf("%s %s/%s%s (%s)", i.Kind, namespace, i.Name, occurrence, i.APIVersion)
}

type indexedKey struct {
	Identity   ObjectIdentity
	Occurrence int
}

func index(records []engine.ObjectRecord) map[indexedKey]engine.ObjectRecord {
	result := make(map[indexedKey]engine.ObjectRecord, len(records))
	occurrences := map[ObjectIdentity]int{}
	for _, record := range records {
		identity := identityOf(record.Object)
		occurrence := occurrences[identity]
		occurrences[identity]++
		result[indexedKey{Identity: identity, Occurrence: occurrence}] = record
	}
	return result
}

func identityOf(object api.Object) ObjectIdentity {
	return ObjectIdentity{
		APIVersion: object.APIVersion(), Kind: object.Kind(), Namespace: object.Namespace(), Name: object.Name(),
	}
}

func unionKeys(left, right map[indexedKey]engine.ObjectRecord) []indexedKey {
	seen := map[indexedKey]struct{}{}
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	keys := make([]indexedKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.Identity.APIVersion != right.Identity.APIVersion {
			return left.Identity.APIVersion < right.Identity.APIVersion
		}
		if left.Identity.Kind != right.Identity.Kind {
			return left.Identity.Kind < right.Identity.Kind
		}
		if left.Identity.Namespace != right.Identity.Namespace {
			return left.Identity.Namespace < right.Identity.Namespace
		}
		if left.Identity.Name != right.Identity.Name {
			return left.Identity.Name < right.Identity.Name
		}
		return left.Occurrence < right.Occurrence
	})
	return keys
}

func diffObject(base, head api.Object) []FieldChange {
	var changes []FieldChange
	diffValues("", base.Data, true, head.Data, true, base.Kind() == "Secret" && head.Kind() == "Secret", &changes)
	return changes
}

func diffValues(path string, before any, beforePresent bool, after any, afterPresent bool, secret bool, changes *[]FieldChange) {
	if beforePresent != afterPresent {
		*changes = append(*changes, FieldChange{
			Path: pathOrRoot(path), Before: before, After: after, BeforePresent: beforePresent, AfterPresent: afterPresent,
		})
		return
	}
	if reflect.DeepEqual(before, after) {
		return
	}
	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		keys := map[string]struct{}{}
		for key := range beforeMap {
			keys[key] = struct{}{}
		}
		for key := range afterMap {
			keys[key] = struct{}{}
		}
		ordered := make([]string, 0, len(keys))
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			childPath := path + "/" + escapeJSONPointer(key)
			beforeValue, beforeHasValue := beforeMap[key]
			afterValue, afterHasValue := afterMap[key]
			if secret && (childPath == "/data" || childPath == "/stringData") {
				if beforeHasValue != afterHasValue || !reflect.DeepEqual(beforeValue, afterValue) {
					*changes = append(*changes, FieldChange{Path: childPath, BeforePresent: beforeHasValue, AfterPresent: afterHasValue, Redacted: true})
				}
				continue
			}
			diffValues(childPath, beforeValue, beforeHasValue, afterValue, afterHasValue, secret, changes)
		}
		return
	}
	beforeSlice, beforeIsSlice := before.([]any)
	afterSlice, afterIsSlice := after.([]any)
	if beforeIsSlice && afterIsSlice {
		length := max(len(beforeSlice), len(afterSlice))
		for index := range length {
			beforePresent := index < len(beforeSlice)
			afterPresent := index < len(afterSlice)
			var beforeValue, afterValue any
			if beforePresent {
				beforeValue = beforeSlice[index]
			}
			if afterPresent {
				afterValue = afterSlice[index]
			}
			diffValues(fmt.Sprintf("%s/%d", path, index), beforeValue, beforePresent, afterValue, afterPresent, secret, changes)
		}
		return
	}
	*changes = append(*changes, FieldChange{Path: pathOrRoot(path), Before: before, After: after, BeforePresent: true, AfterPresent: true})
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func pathOrRoot(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func compareStrings(base, head []string) (added, removed []string) {
	baseSet := map[string]struct{}{}
	headSet := map[string]struct{}{}
	for _, value := range base {
		baseSet[value] = struct{}{}
	}
	for _, value := range head {
		headSet[value] = struct{}{}
	}
	for value := range headSet {
		if _, exists := baseSet[value]; !exists {
			added = append(added, value)
		}
	}
	for value := range baseSet {
		if _, exists := headSet[value]; !exists {
			removed = append(removed, value)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func formatFieldValue(value any, present bool) string {
	if !present {
		return "<absent>"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func formatRedactedValue(present bool) string {
	if !present {
		return "<absent>"
	}
	return "<redacted>"
}
