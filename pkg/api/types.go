package api

import (
	"context"
	"fmt"
	"strings"
)

// Object is one Kubernetes-style YAML document.
type Object struct {
	Data map[string]any
}

func (o Object) APIVersion() string { return stringValue(o.Data, "apiVersion") }
func (o Object) Kind() string       { return stringValue(o.Data, "kind") }

func (o Object) Name() string {
	metadata, _ := o.Data["metadata"].(map[string]any)
	return stringValue(metadata, "name")
}

func (o Object) Namespace() string {
	metadata, _ := o.Data["metadata"].(map[string]any)
	return stringValue(metadata, "namespace")
}

func (o Object) Key() ObjectKey {
	group := ""
	if separator := strings.IndexByte(o.APIVersion(), '/'); separator >= 0 {
		group = o.APIVersion()[:separator]
	}
	return ObjectKey{Group: group, Kind: o.Kind(), Namespace: o.Namespace(), Name: o.Name()}
}

func (o Object) Annotation(name string) string {
	metadata, _ := o.Data["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	return stringValue(annotations, name)
}

func (o Object) Label(name string) string {
	metadata, _ := o.Data["metadata"].(map[string]any)
	labels, _ := metadata["labels"].(map[string]any)
	return stringValue(labels, name)
}

type ObjectKey struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

func (k ObjectKey) String() string {
	namespace := k.Namespace
	if namespace == "" {
		namespace = "<cluster>"
	}
	group := k.Group
	if group == "" {
		group = "core"
	}
	return fmt.Sprintf("%s, Kind=%s %s/%s", group, k.Kind, namespace, k.Name)
}

type SourceQuery struct {
	Adapter string
	Fields  map[string]string
}

type LocalSource struct {
	Name    string
	Path    string
	Ignored bool
}

type SourceResolver interface {
	Resolve(SourceQuery) (LocalSource, error)
}

type ObjectLookup interface {
	Get(kind, namespace, name string) (Object, bool)
}

type Unit struct {
	ID        string
	Adapter   string
	Object    Object
	DependsOn []string
}

type RenderRequest struct {
	Renderer  string
	Source    LocalSource
	Path      string
	Recursive bool
}

type RenderResult struct {
	Request RenderRequest
	Objects []Object
}

type Capability struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

// Adapter translates a GitOps controller resource into generic render work.
// Implementations own all controller-specific discovery and transformation.
type Adapter interface {
	Name() string
	Capabilities() []Capability
	Discover([]Object) ([]Unit, error)
	// Plan returns no requests only when every source used by the unit was
	// explicitly ignored by source policy. Transform is not called in that case.
	Plan(Unit, SourceResolver) ([]RenderRequest, error)
	Transform(Unit, []RenderResult, ObjectLookup) ([]Object, error)
}

// EntrypointTransformer is an optional adapter capability for controller-owned
// transformations that must happen before the first discovery pass.
type EntrypointTransformer interface {
	TransformEntrypoint([]Object, ObjectLookup) ([]Object, error)
}

// Renderer turns a local source path into Kubernetes-style objects.
type Renderer interface {
	Name() string
	Capabilities() []Capability
	Detect(RenderRequest) (bool, error)
	Render(context.Context, RenderRequest) ([]Object, error)
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}
