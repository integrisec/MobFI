package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/integrisec/MobFI/internal/plist"
)

// plistDiffer compares two property lists (binary or XML) structurally,
// reusing the JSON field-diff over their decoded values.
type plistDiffer struct{}

func (plistDiffer) Handles(path string) bool {
	head := plistHead(path, 1024)
	if plist.IsBinary(head) || plist.LooksXML(head) {
		return true
	}
	return strings.EqualFold(filepath.Ext(path), ".plist")
}

func plistHead(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	head := make([]byte, n)
	m, _ := f.Read(head)
	return head[:m]
}

func (plistDiffer) Diff(_ context.Context, aPath, bPath string) (string, error) {
	va, err := loadPlist(aPath)
	if err != nil {
		return "", err
	}
	vb, err := loadPlist(bPath)
	if err != nil {
		return "", err
	}
	var c jsonCounts
	diffJSON(va, vb, &c)
	if c.changed == 0 && c.added == 0 && c.removed == 0 {
		return "plist: no field differences", nil
	}
	return fmt.Sprintf("plist: %d changed, %d added, %d removed field(s)", c.changed, c.added, c.removed), nil
}

func loadPlist(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return plist.DecodeAny(b)
}
