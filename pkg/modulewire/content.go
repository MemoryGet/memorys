package modulewire

import (
	"reflect"

	"github.com/MemoryGet/memorys/pkg/contracts"
)

func copyObject(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func flatten(kind reflect.Type, object map[string]any) (map[string]any, error) {
	switch kind {
	case reflect.TypeOf(contracts.ParsedBlock{}):
		content, ok := object["content"].(map[string]any)
		if !ok {
			return nil, invalid()
		}
		delete(object, "content")
		for key, value := range content {
			object[key] = value
		}
	case reflect.TypeOf(contracts.Content{}):
		return flattenContent(object)
	case reflect.TypeOf(contracts.Locator{}), reflect.TypeOf(contracts.BlockSpan{}):
		if characters, present := object["characters"]; present {
			span := characters.(map[string]any)
			object["char_start"], object["char_end"] = span["start"], span["end"]
			delete(object, "characters")
		}
		if box, present := object["box"]; present {
			values := box.(map[string]any)
			object["bbox"] = []any{values["left"], values["top"], values["right"], values["bottom"]}
			delete(object, "box")
		}
	case reflect.TypeOf(contracts.SourceAnchor{}):
		if err := sourceKind(object); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func flattenContent(object map[string]any) (map[string]any, error) {
	kind, ok := object["kind"].(string)
	if !ok {
		return nil, invalid()
	}
	count := 0
	for _, name := range []string{"table", "image", "audio"} {
		if _, present := object[name]; present {
			count++
		}
	}
	if kind == "text" {
		if count != 0 {
			return nil, invalid()
		}
		return object, nil
	}
	if kind != "table" && kind != "image" && kind != "audio" {
		return nil, invalid()
	}
	payload, present := object[kind].(map[string]any)
	if !present || count != 1 {
		return nil, invalid()
	}
	payload["type"] = kind
	object["structured_content"] = payload
	delete(object, kind)
	return object, nil
}

func expand(kind reflect.Type, original map[string]any) (map[string]any, error) {
	object := copyObject(original)
	switch kind {
	case reflect.TypeOf(contracts.ParsedBlock{}):
		if _, exists := object["content"]; exists {
			return nil, invalid()
		}
		content := make(map[string]any)
		for _, key := range []string{"kind", "text", "source_anchor", "structured_content"} {
			if value, exists := object[key]; exists {
				content[key] = value
				delete(object, key)
			}
		}
		object["content"] = content
	case reflect.TypeOf(contracts.Content{}):
		return expandContent(object)
	case reflect.TypeOf(contracts.Locator{}), reflect.TypeOf(contracts.BlockSpan{}):
		return expandLocator(object)
	case reflect.TypeOf(contracts.SourceAnchor{}):
		if err := sourceKind(object); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func expandContent(object map[string]any) (map[string]any, error) {
	for _, key := range []string{"table", "image", "audio"} {
		if _, exists := object[key]; exists {
			return nil, invalid()
		}
	}
	kind, ok := object["kind"].(string)
	if !ok {
		return nil, invalid()
	}
	payload, present := object["structured_content"]
	if kind == "text" {
		if present {
			return nil, invalid()
		}
		return object, nil
	}
	if kind != "table" && kind != "image" && kind != "audio" {
		return nil, invalid()
	}
	values, ok := payload.(map[string]any)
	if !present || !ok || values["type"] != kind {
		return nil, invalid()
	}
	values = copyObject(values)
	delete(values, "type")
	object[kind] = values
	delete(object, "structured_content")
	return object, nil
}

func expandLocator(object map[string]any) (map[string]any, error) {
	if _, exists := object["characters"]; exists {
		return nil, invalid()
	}
	if _, exists := object["box"]; exists {
		return nil, invalid()
	}
	start, hasStart := object["char_start"]
	end, hasEnd := object["char_end"]
	if hasStart != hasEnd {
		return nil, invalid()
	}
	if hasStart {
		object["characters"] = map[string]any{"start": start, "end": end}
		delete(object, "char_start")
		delete(object, "char_end")
	}
	if box, exists := object["bbox"]; exists {
		values, ok := box.([]any)
		if !ok || len(values) != 4 {
			return nil, invalid()
		}
		object["box"] = map[string]any{"left": values[0], "top": values[1], "right": values[2], "bottom": values[3]}
		delete(object, "bbox")
	}
	return object, nil
}

func sourceKind(object map[string]any) error {
	_, blob := object["blob_id"]
	_, revision := object["revision_block_id"]
	if object["kind"] == "original" && blob && !revision {
		return nil
	}
	if object["kind"] == "revision" && !blob && revision {
		return nil
	}
	return invalid()
}
