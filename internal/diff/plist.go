package diff

import (
	"context"
	"fmt"
	"os"

	"github.com/integrisec/MobFI/internal/plist"
)

// plistDiffer compares two binary property lists structurally, reusing the
// JSON field-diff over their decoded values. It handles only binary plists
// (by magic); XML plists are left to the byte-level fallback.
type plistDiffer struct{}

func (plistDiffer) Handles(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, len(plist.Magic))
	n, _ := f.Read(head)
	return plist.IsBinary(head[:n])
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
	return plist.Decode(b)
}
