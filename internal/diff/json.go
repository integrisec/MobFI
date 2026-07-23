package diff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// jsonDiffer compares two JSON documents structurally, counting how many
// leaf fields changed, were added, or were removed. Objects are matched by
// key; arrays are matched by index with length differences counted as
// added/removed.
type jsonDiffer struct{}

func (jsonDiffer) Handles(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

func (jsonDiffer) Diff(_ context.Context, aPath, bPath string) (string, error) {
	va, err := loadJSON(aPath)
	if err != nil {
		return "", err
	}
	vb, err := loadJSON(bPath)
	if err != nil {
		return "", err
	}
	var c jsonCounts
	diffJSON(va, vb, &c)
	if c.changed == 0 && c.added == 0 && c.removed == 0 {
		return "json: no field differences", nil
	}
	return fmt.Sprintf("json: %d changed, %d added, %d removed field(s)", c.changed, c.added, c.removed), nil
}

func loadJSON(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

type jsonCounts struct{ changed, added, removed int }

func diffJSON(a, b any, c *jsonCounts) {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			c.changed++
			return
		}
		for k, avv := range av {
			if bvv, ok := bv[k]; ok {
				diffJSON(avv, bvv, c)
			} else {
				c.removed++
			}
		}
		for k := range bv {
			if _, ok := av[k]; !ok {
				c.added++
			}
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			c.changed++
			return
		}
		n := len(av)
		if len(bv) < n {
			n = len(bv)
		}
		for i := 0; i < n; i++ {
			diffJSON(av[i], bv[i], c)
		}
		switch {
		case len(bv) > len(av):
			c.added += len(bv) - len(av)
		case len(av) > len(bv):
			c.removed += len(av) - len(bv)
		}
	default:
		if !reflect.DeepEqual(a, b) {
			c.changed++
		}
	}
}
