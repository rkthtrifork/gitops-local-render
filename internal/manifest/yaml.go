package manifest

import (
	"bytes"
	"fmt"
	"io"

	"github.com/rkthtrifork/gitops-local-render/pkg/api"
	"gopkg.in/yaml.v3"
)

func Parse(data []byte) ([]api.Object, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var objects []api.Object

	for document := 1; ; document++ {
		var value map[string]any
		err := decoder.Decode(&value)
		if err == io.EOF {
			return objects, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode YAML document %d: %w", document, err)
		}
		if len(value) == 0 {
			continue
		}
		objects = append(objects, api.Object{Data: value})
	}
}

func MarshalObject(object api.Object) ([]byte, error) {
	data, err := yaml.Marshal(object.Data)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", object.Key(), err)
	}
	return data, nil
}

func Marshal(objects []api.Object) ([]byte, error) {
	var output bytes.Buffer
	for index, object := range objects {
		data, err := MarshalObject(object)
		if err != nil {
			return nil, err
		}
		if index > 0 {
			output.WriteString("---\n")
		}
		output.Write(data)
	}
	return output.Bytes(), nil
}
