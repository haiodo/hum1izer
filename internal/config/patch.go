package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Patch меняет значения в .hum1izer.yaml через yaml.Node: маршалинг структуры
// выбросил бы комментарии. Ключи - точечные пути, "comments.max_lines".
func Patch(path string, kv map[string]any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: ожидался словарь на верхнем уровне", path)
	}

	for key, val := range kv {
		if err := setPath(doc.Content[0], strings.Split(key, "."), val); err != nil {
			return fmt.Errorf("%s: %s: %w", path, key, err)
		}
	}

	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func setPath(m *yaml.Node, keys []string, val any) error {
	if len(keys) == 0 {
		return errors.New("пустой путь")
	}
	child := findKey(m, keys[0])
	if len(keys) == 1 {
		if child == nil {
			m.Content = append(m.Content, scalar(keys[0]), valueNode(val))
			return nil
		}
		*child = *valueNode(val)
		return nil
	}
	if child == nil {
		child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		m.Content = append(m.Content, scalar(keys[0]), child)
	}
	if child.Kind != yaml.MappingNode {
		return fmt.Errorf("%q не словарь", keys[0])
	}
	return setPath(child, keys[1:], val)
}

func findKey(m *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == name {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func valueNode(v any) *yaml.Node {
	if list, ok := v.([]string); ok {
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, s := range list {
			n.Content = append(n.Content, scalar(s))
		}
		return n
	}
	n := &yaml.Node{Kind: yaml.ScalarNode}
	switch x := v.(type) {
	case bool:
		n.Tag, n.Value = "!!bool", fmt.Sprint(x)
	case int:
		n.Tag, n.Value = "!!int", fmt.Sprint(x)
	default:
		n.Tag, n.Value = "!!str", fmt.Sprint(x)
	}
	return n
}
