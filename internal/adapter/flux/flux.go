package flux

import (
	"encoding/base64"
	"fmt"

	fluxenv "github.com/fluxcd/pkg/envsubst"
	adapterutil "github.com/rkthtrifork/gitops-local-render/internal/adapter"
	"github.com/rkthtrifork/gitops-local-render/internal/manifest"
	"github.com/rkthtrifork/gitops-local-render/pkg/api"
)

const (
	name       = "flux"
	apiVersion = "kustomize.toolkit.fluxcd.io/v1"
)

type Adapter struct {
	Entrypoint *Entrypoint
}

type Entrypoint struct {
	Namespace          string
	SubstituteFrom     []SubstituteReference
	Substitute         map[string]string
	SubstituteStrategy string
}

type SubstituteReference struct {
	Kind     string
	Name     string
	Optional bool
}

func (Adapter) Name() string { return name }

func (Adapter) Capabilities() []api.Capability {
	return []api.Capability{
		{Name: "kustomization", Description: "Discovers kustomize.toolkit.fluxcd.io/v1 Kustomizations"},
		{Name: "post-build-substitution", Description: "Implements strict Flux envsubst with substituteFrom and inline precedence"},
		{Name: "depends-on", Description: "Orders locally discovered Flux Kustomizations by dependsOn"},
		{Name: "entrypoint-substitution", Description: "Applies an explicit bootstrap substitution scope before initial discovery"},
	}
}

func (a Adapter) TransformEntrypoint(objects []api.Object, store api.ObjectLookup) ([]api.Object, error) {
	if a.Entrypoint == nil {
		return objects, nil
	}
	postBuild := map[string]any{
		"substituteStrategy": a.Entrypoint.SubstituteStrategy,
	}
	if len(a.Entrypoint.Substitute) > 0 {
		values := make(map[string]any, len(a.Entrypoint.Substitute))
		for key, value := range a.Entrypoint.Substitute {
			values[key] = value
		}
		postBuild["substitute"] = values
	}
	if len(a.Entrypoint.SubstituteFrom) > 0 {
		references := make([]any, 0, len(a.Entrypoint.SubstituteFrom))
		for _, reference := range a.Entrypoint.SubstituteFrom {
			references = append(references, map[string]any{
				"kind": reference.Kind, "name": reference.Name, "optional": reference.Optional,
			})
		}
		postBuild["substituteFrom"] = references
	}
	synthetic := api.Unit{Object: api.Object{Data: map[string]any{
		"apiVersion": apiVersion,
		"kind":       "Kustomization",
		"metadata": map[string]any{
			"name": "entrypoint", "namespace": a.Entrypoint.Namespace,
		},
		"spec": map[string]any{"postBuild": postBuild},
	}}}
	return a.Transform(synthetic, []api.RenderResult{{Objects: objects}}, store)
}

func (Adapter) Discover(objects []api.Object) ([]api.Unit, error) {
	var units []api.Unit
	for _, object := range objects {
		if object.Kind() != "Kustomization" {
			continue
		}
		if object.APIVersion() != apiVersion {
			if hasFluxAPIGroup(object.APIVersion()) {
				return nil, fmt.Errorf("unsupported Flux Kustomization apiVersion %q for %s/%s", object.APIVersion(), object.Namespace(), object.Name())
			}
			continue
		}
		namespace := object.Namespace()
		if namespace == "" {
			namespace = "default"
		}
		if object.Name() == "" {
			return nil, fmt.Errorf("Flux Kustomization has no metadata.name")
		}
		dependencies, err := dependencies(object, namespace)
		if err != nil {
			return nil, fmt.Errorf("Flux Kustomization %s/%s: %w", namespace, object.Name(), err)
		}
		units = append(units, api.Unit{
			ID:        unitID(namespace, object.Name()),
			Adapter:   name,
			Object:    object,
			DependsOn: dependencies,
		})
	}
	return units, nil
}

func (Adapter) Plan(unit api.Unit, resolver api.SourceResolver) ([]api.RenderRequest, error) {
	spec, err := adapterutil.Map(unit.Object.Data["spec"], "spec")
	if err != nil {
		return nil, err
	}
	sourceRef, err := adapterutil.Map(spec["sourceRef"], "spec.sourceRef")
	if err != nil {
		return nil, err
	}
	namespace := adapterutil.String(sourceRef, "namespace")
	if namespace == "" {
		namespace = unit.Object.Namespace()
		if namespace == "" {
			namespace = "default"
		}
	}
	kind := adapterutil.String(sourceRef, "kind")
	sourceName := adapterutil.String(sourceRef, "name")
	if kind == "" || sourceName == "" {
		return nil, fmt.Errorf("spec.sourceRef.kind and spec.sourceRef.name are required")
	}
	source, err := resolver.Resolve(api.SourceQuery{Adapter: name, Fields: map[string]string{
		"kind": kind, "namespace": namespace, "name": sourceName,
	}})
	if err != nil {
		return nil, err
	}
	if source.Ignored {
		return nil, nil
	}
	path := adapterutil.String(spec, "path")
	if path == "" {
		path = "."
	}
	return []api.RenderRequest{{Renderer: "auto", Source: source, Path: path, Recursive: true}}, nil
}

func (Adapter) Transform(unit api.Unit, results []api.RenderResult, store api.ObjectLookup) ([]api.Object, error) {
	if len(results) != 1 {
		return nil, fmt.Errorf("expected one render result, got %d", len(results))
	}
	spec, err := adapterutil.Map(unit.Object.Data["spec"], "spec")
	if err != nil {
		return nil, err
	}
	postBuild := adapterutil.OptionalMap(spec["postBuild"])
	variables, err := substitutionVariables(unit, postBuild, store)
	if err != nil {
		return nil, err
	}
	strategy := adapterutil.String(postBuild, "substituteStrategy")
	if strategy == "" {
		strategy = "WithVariables"
	}
	if strategy != "WithVariables" && strategy != "Always" {
		return nil, fmt.Errorf("unsupported spec.postBuild.substituteStrategy %q", strategy)
	}
	if len(variables) == 0 && strategy == "WithVariables" {
		return results[0].Objects, nil
	}

	transformed := make([]api.Object, 0, len(results[0].Objects))
	for _, object := range results[0].Objects {
		if substitutionDisabled(object) {
			transformed = append(transformed, object)
			continue
		}
		data, err := manifest.MarshalObject(object)
		if err != nil {
			return nil, err
		}
		expanded, err := fluxenv.Eval(string(data), func(key string) (string, bool) {
			value, found := variables[key]
			return value, found
		})
		if err != nil {
			return nil, fmt.Errorf("substitute %s: %w", object.Key(), err)
		}
		objects, err := manifest.Parse([]byte(expanded))
		if err != nil {
			return nil, fmt.Errorf("parse substituted %s: %w", object.Key(), err)
		}
		if len(objects) != 1 {
			return nil, fmt.Errorf("substitution changed document count for %s", object.Key())
		}
		transformed = append(transformed, objects[0])
	}
	return transformed, nil
}

func substitutionVariables(unit api.Unit, postBuild map[string]any, store api.ObjectLookup) (map[string]string, error) {
	variables := map[string]string{}
	namespace := unit.Object.Namespace()
	if namespace == "" {
		namespace = "default"
	}
	if rawReferences, exists := postBuild["substituteFrom"]; exists {
		references, err := adapterutil.Slice(rawReferences, "spec.postBuild.substituteFrom")
		if err != nil {
			return nil, err
		}
		for index, rawReference := range references {
			reference, err := adapterutil.Map(rawReference, fmt.Sprintf("spec.postBuild.substituteFrom[%d]", index))
			if err != nil {
				return nil, err
			}
			kind := adapterutil.String(reference, "kind")
			if kind == "" {
				kind = "ConfigMap"
			}
			if kind != "ConfigMap" && kind != "Secret" {
				return nil, fmt.Errorf("substituteFrom[%d] has unsupported kind %q", index, kind)
			}
			objectName := adapterutil.String(reference, "name")
			if objectName == "" {
				return nil, fmt.Errorf("substituteFrom[%d].name is required", index)
			}
			object, found := store.Get(kind, namespace, objectName)
			if !found {
				if adapterutil.Bool(reference, "optional") {
					continue
				}
				return nil, fmt.Errorf("required substituteFrom %s/%s/%s was not found", kind, namespace, objectName)
			}
			values, err := objectVariables(object)
			if err != nil {
				return nil, fmt.Errorf("read substituteFrom %s/%s/%s: %w", kind, namespace, objectName, err)
			}
			for key, value := range values {
				variables[key] = value
			}
		}
	}
	if inline := adapterutil.OptionalMap(postBuild["substitute"]); inline != nil {
		for key, rawValue := range inline {
			value, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("spec.postBuild.substitute.%s must be a string", key)
			}
			variables[key] = value
		}
	}
	return variables, nil
}

func objectVariables(object api.Object) (map[string]string, error) {
	variables := map[string]string{}
	data := adapterutil.OptionalMap(object.Data["data"])
	switch object.Kind() {
	case "ConfigMap":
		for key, rawValue := range data {
			value, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("data.%s must be a string", key)
			}
			variables[key] = value
		}
	case "Secret":
		for key, rawValue := range data {
			encoded, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("data.%s must be a base64 string", key)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf("decode data.%s: %w", key, err)
			}
			variables[key] = string(decoded)
		}
		for key, rawValue := range adapterutil.OptionalMap(object.Data["stringData"]) {
			value, ok := rawValue.(string)
			if !ok {
				return nil, fmt.Errorf("stringData.%s must be a string", key)
			}
			variables[key] = value
		}
	default:
		return nil, fmt.Errorf("unsupported object kind %q", object.Kind())
	}
	return variables, nil
}

func dependencies(object api.Object, namespace string) ([]string, error) {
	spec, err := adapterutil.Map(object.Data["spec"], "spec")
	if err != nil {
		return nil, err
	}
	raw, exists := spec["dependsOn"]
	if !exists {
		return nil, nil
	}
	items, err := adapterutil.Slice(raw, "spec.dependsOn")
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for index, item := range items {
		dependency, err := adapterutil.Map(item, fmt.Sprintf("spec.dependsOn[%d]", index))
		if err != nil {
			return nil, err
		}
		dependencyNamespace := adapterutil.String(dependency, "namespace")
		if dependencyNamespace == "" {
			dependencyNamespace = namespace
		}
		dependencyName := adapterutil.String(dependency, "name")
		if dependencyName == "" {
			return nil, fmt.Errorf("spec.dependsOn[%d].name is required", index)
		}
		result = append(result, unitID(dependencyNamespace, dependencyName))
	}
	return result, nil
}

func substitutionDisabled(object api.Object) bool {
	const key = "kustomize.toolkit.fluxcd.io/substitute"
	return object.Annotation(key) == "disabled" || object.Label(key) == "disabled"
}

func unitID(namespace, objectName string) string {
	return fmt.Sprintf("flux:%s/%s", namespace, objectName)
}

func hasFluxAPIGroup(version string) bool {
	return len(version) > len("kustomize.toolkit.fluxcd.io/") && version[:len("kustomize.toolkit.fluxcd.io/")] == "kustomize.toolkit.fluxcd.io/"
}
