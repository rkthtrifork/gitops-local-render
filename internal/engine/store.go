package engine

import "github.com/rkthtrifork/gitops-local-render/pkg/api"

type objectStore struct {
	objects map[storeKey]api.Object
}

type storeKey struct {
	kind      string
	namespace string
	name      string
}

func newObjectStore() *objectStore {
	return &objectStore{objects: map[storeKey]api.Object{}}
}

func (s *objectStore) Get(kind, namespace, name string) (api.Object, bool) {
	object, found := s.objects[storeKey{kind: kind, namespace: namespace, name: name}]
	return object, found
}

func (s *objectStore) put(objects []api.Object) {
	for _, object := range objects {
		if object.Kind() == "" || object.Name() == "" {
			continue
		}
		s.objects[storeKey{kind: object.Kind(), namespace: object.Namespace(), name: object.Name()}] = object
	}
}
