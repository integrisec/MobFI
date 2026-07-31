// Package plist decodes Apple binary property lists (bplist00) into plain
// Go values (map[string]any, []any, string, int64, float64, bool, []byte,
// time.Time, UID). It is read-only and defensive: malformed input yields an
// error, never a panic, since plists come from untrusted device data.
package plist

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"time"
	"unicode/utf16"
)

// Magic is the 8-byte header of a binary property list.
var Magic = []byte("bplist00")

// cocoaEpoch is 2001-01-01 UTC, the reference date for plist dates.
var cocoaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// UID is a CoreFoundation keyed-archiver unique id. It marshals to JSON as
// {"CF$UID": n}, matching plistlib/NSKeyedArchiver conventions.
type UID uint64

// MarshalJSON renders the UID in the CF$UID form.
func (u UID) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"CF$UID":%d}`, uint64(u))), nil
}

// IsBinary reports whether b begins with the binary-plist magic.
func IsBinary(b []byte) bool { return bytes.HasPrefix(b, Magic) }

// Decode parses a binary property list and returns its root value.
func Decode(data []byte) (v any, err error) {
	// Defence in depth: never let a malformed plist panic the caller.
	defer func() {
		if r := recover(); r != nil {
			v, err = nil, fmt.Errorf("plist: malformed input: %v", r)
		}
	}()

	if !IsBinary(data) {
		return nil, errors.New("plist: not a binary plist")
	}
	if len(data) < len(Magic)+32 {
		return nil, errors.New("plist: too short")
	}

	trailer := data[len(data)-32:]
	offsetIntSize := int(trailer[6])
	objRefSize := int(trailer[7])
	numObjects := binary.BigEndian.Uint64(trailer[8:16])
	topObject := binary.BigEndian.Uint64(trailer[16:24])
	offsetTableOffset := binary.BigEndian.Uint64(trailer[24:32])

	if offsetIntSize < 1 || offsetIntSize > 8 || objRefSize < 1 || objRefSize > 8 {
		return nil, errors.New("plist: bad trailer sizes")
	}
	// MFI-PAR-04: numObjects * offsetIntSize is attacker-controlled; the
	// multiplication wraps in uint64 and can bypass the range check that
	// follows. math/bits.Mul64 gives us the (hi, lo) parts so we can reject
	// overflow before it becomes a giant allocation via make([]uint64,
	// numObjects) below.
	hi, low := bits.Mul64(numObjects, uint64(offsetIntSize))
	if hi != 0 {
		return nil, errors.New("plist: numObjects * offsetIntSize overflows uint64")
	}
	end := offsetTableOffset + low
	if end < offsetTableOffset {
		return nil, errors.New("plist: offset-table end overflows uint64")
	}
	if numObjects == 0 || end > uint64(len(data)) || topObject >= numObjects {
		return nil, errors.New("plist: offset table out of range")
	}
	// Additional sanity gate for the []uint64 allocation on line below: a
	// malicious numObjects near uint64 max still fits the previous check if
	// offsetIntSize is 1, but 8*numObjects for the []uint64 backing store
	// would exhaust the process. Cap numObjects at len(data), which is the
	// theoretical maximum count of objects that could fit.
	if numObjects > uint64(len(data)) {
		return nil, errors.New("plist: numObjects exceeds file size")
	}

	offsets := make([]uint64, numObjects)
	p := offsetTableOffset
	for i := uint64(0); i < numObjects; i++ {
		offsets[i] = readUint(data[p : p+uint64(offsetIntSize)])
		p += uint64(offsetIntSize)
	}

	d := &decoder{data: data, offsets: offsets, refSize: objRefSize, visiting: map[uint64]bool{}}
	return d.object(topObject, 0)
}

// maxPlistDepth caps binary-plist recursion. The cycle-detection map catches
// direct back-references but not depth: N nested arrays / dicts each fit in
// a few bytes, so a crafted fixture pushes Go's stack into throw("stack
// overflow"), which is NOT recoverable via the outer defer. See MFI-PAR-01.
const maxPlistDepth = 128

type decoder struct {
	data     []byte
	offsets  []uint64
	refSize  int
	visiting map[uint64]bool
}

func (d *decoder) at(off, n uint64) []byte {
	if off+n > uint64(len(d.data)) {
		panic("read past end of data")
	}
	return d.data[off : off+n]
}

func (d *decoder) object(index uint64, depth int) (any, error) {
	if depth > maxPlistDepth {
		return nil, fmt.Errorf("plist: nested too deep (> %d)", maxPlistDepth)
	}
	if index >= uint64(len(d.offsets)) {
		return nil, fmt.Errorf("plist: object index %d out of range", index)
	}
	if d.visiting[index] {
		return nil, errors.New("plist: cyclic reference")
	}
	off := d.offsets[index]
	marker := d.at(off, 1)[0]
	hi, lo := marker&0xF0, marker&0x0F

	switch hi {
	case 0x00:
		switch marker {
		case 0x00, 0x0F:
			return nil, nil // null / fill
		case 0x08:
			return false, nil
		case 0x09:
			return true, nil
		}
		return nil, fmt.Errorf("plist: unknown marker 0x%02x", marker)

	case 0x10: // int (1<<lo bytes, big-endian)
		n := uint64(1) << lo
		return int64(readUint(d.at(off+1, n))), nil

	case 0x20: // real
		n := uint64(1) << lo
		return d.real(off+1, n)

	case 0x30: // date: 8-byte big-endian float64 seconds since 2001-01-01
		secs := math.Float64frombits(binary.BigEndian.Uint64(d.at(off+1, 8)))
		return cocoaEpoch.Add(time.Duration(secs * float64(time.Second))), nil

	case 0x40: // data
		count, start := d.sizeAndStart(off, lo)
		return append([]byte(nil), d.at(start, count)...), nil

	case 0x50: // ASCII string
		count, start := d.sizeAndStart(off, lo)
		return string(d.at(start, count)), nil

	case 0x60: // UTF-16BE string (count = code units)
		count, start := d.sizeAndStart(off, lo)
		if count > uint64(len(d.data)) {
			return nil, fmt.Errorf("plist: utf-16 string count %d exceeds file size", count)
		}
		u16 := make([]uint16, count)
		b := d.at(start, count*2)
		for i := uint64(0); i < count; i++ {
			u16[i] = binary.BigEndian.Uint16(b[i*2:])
		}
		return string(utf16.Decode(u16)), nil

	case 0x80: // UID ((lo+1) bytes)
		n := uint64(lo) + 1
		return UID(readUint(d.at(off+1, n))), nil

	case 0xA0, 0xC0: // array / set
		return d.collection(index, off, lo, depth+1)

	case 0xD0: // dict
		return d.dict(index, off, lo, depth+1)
	}
	return nil, fmt.Errorf("plist: unknown marker 0x%02x", marker)
}

func (d *decoder) collection(index, off uint64, lo byte, depth int) (any, error) {
	count, start := d.sizeAndStart(off, lo)
	// MFI-PAR-05: count is attacker-controlled; make([]any, count) allocates
	// 16 bytes/elem. Cap by the file size (an object cannot legitimately
	// hold more entries than the total byte length of the file).
	if count > uint64(len(d.data)) {
		return nil, fmt.Errorf("plist: collection count %d exceeds file size", count)
	}
	d.visiting[index] = true
	defer delete(d.visiting, index)

	out := make([]any, count)
	rs := uint64(d.refSize)
	refs := d.at(start, count*rs)
	for i := uint64(0); i < count; i++ {
		v, err := d.object(readUint(refs[i*rs:(i+1)*rs]), depth)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (d *decoder) dict(index, off uint64, lo byte, depth int) (any, error) {
	count, start := d.sizeAndStart(off, lo)
	if count > uint64(len(d.data)) {
		return nil, fmt.Errorf("plist: dict count %d exceeds file size", count)
	}
	d.visiting[index] = true
	defer delete(d.visiting, index)

	rs := uint64(d.refSize)
	keyRefs := d.at(start, count*rs)
	valRefs := d.at(start+count*rs, count*rs)
	out := make(map[string]any, count)
	for i := uint64(0); i < count; i++ {
		k, err := d.object(readUint(keyRefs[i*rs:(i+1)*rs]), depth)
		if err != nil {
			return nil, err
		}
		v, err := d.object(readUint(valRefs[i*rs:(i+1)*rs]), depth)
		if err != nil {
			return nil, err
		}
		out[keyString(k)] = v
	}
	return out, nil
}

func (d *decoder) real(off, n uint64) (any, error) {
	switch n {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(d.at(off, 4)))), nil
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(d.at(off, 8))), nil
	}
	return nil, fmt.Errorf("plist: bad real size %d", n)
}

// sizeAndStart resolves the element count for a marker and the offset where
// its payload begins. A low nibble of 0xF means the real count follows as an
// inline int object.
func (d *decoder) sizeAndStart(off uint64, lo byte) (count, start uint64) {
	if lo != 0x0F {
		return uint64(lo), off + 1
	}
	m := d.at(off+1, 1)[0]
	if m&0xF0 != 0x10 {
		panic("bad extended length marker")
	}
	n := uint64(1) << (m & 0x0F)
	count = readUint(d.at(off+2, n))
	return count, off + 2 + n
}

func readUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func keyString(k any) string {
	if s, ok := k.(string); ok {
		return s
	}
	return fmt.Sprint(k)
}
