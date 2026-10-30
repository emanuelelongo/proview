package document

import (
	"encoding/binary"
	"hash/fnv"
	"image"
	"math"
	"sync"
)

// derivedRasterCache retains page rasters derived from immutable source pixels:
// rotations, filled-form renders and redaction-painted thumbnails. Without it
// every Preview/Thumbnail call allocated a fresh full-page copy, and a rotated
// page stayed resident twice (source cache plus the copy shown on screen).
// Keys capture every input, so entries never need invalidation; stale variants
// age out through the byte-bounded LRU.
type derivedRasterCache struct {
	mu      sync.Mutex
	entries map[derivedKey]derivedEntry
	bytes   int64
	clock   uint64
	// One filled-form PDF per source, reused for every page render with the
	// same field values instead of regenerating the whole file per page.
	filled map[*Source]filledPDF
}

type derivedKey struct {
	source     *Source
	index      int
	thumbnail  bool
	turns      int
	forms      uint64
	redactions uint64
}

type derivedEntry struct {
	image image.Image
	used  uint64
}

type filledPDF struct {
	forms uint64
	data  []byte
}

func (c *derivedRasterCache) get(key derivedKey) image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	c.clock++
	entry.used = c.clock
	c.entries[key] = entry
	return entry.image
}

func (c *derivedRasterCache) put(key derivedKey, picture image.Image) {
	if picture == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[derivedKey]derivedEntry)
	}
	if old, ok := c.entries[key]; ok {
		c.bytes -= rasterBytes(old.image)
	}
	c.clock++
	c.entries[key] = derivedEntry{image: picture, used: c.clock}
	c.bytes += rasterBytes(picture)
	limit := sourceRasterLimit()
	for c.bytes > limit && len(c.entries) > 1 {
		var oldest derivedKey
		oldestUsed := ^uint64(0)
		for k, e := range c.entries {
			if e.used < oldestUsed {
				oldest, oldestUsed = k, e.used
			}
		}
		c.bytes -= rasterBytes(c.entries[oldest].image)
		delete(c.entries, oldest)
	}
}

// dropPreview releases full-size derived previews of one source page.
func (c *derivedRasterCache) dropPreview(source *Source, index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if k.source == source && k.index == index && !k.thumbnail {
			c.bytes -= rasterBytes(e.image)
			delete(c.entries, k)
		}
	}
}

// trim releases derived previews and filled-form PDFs; thumbnails are kept
// when previewsOnly is set, matching Source.TrimPreviews.
func (c *derivedRasterCache) trim(previewsOnly bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if !previewsOnly || !k.thumbnail {
			c.bytes -= rasterBytes(e.image)
			delete(c.entries, k)
		}
	}
	c.filled = nil
}

func (c *derivedRasterCache) retainedBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.bytes
	for _, f := range c.filled {
		total += int64(len(f.data))
	}
	return total
}

func (c *derivedRasterCache) filledPDF(source *Source, forms uint64, fill func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if f, ok := c.filled[source]; ok && f.forms == forms {
		c.mu.Unlock()
		return f.data, nil
	}
	c.mu.Unlock()
	data, err := fill()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.filled == nil {
		c.filled = make(map[*Source]filledPDF)
	}
	c.filled[source] = filledPDF{forms: forms, data: data}
	c.mu.Unlock()
	return data, nil
}

// formsFingerprint identifies the field values applied to one source. Zero
// means the source has no edited fields.
func (d *Document) formsFingerprint(source *Source) uint64 {
	var sum uint64
	for key, values := range d.formValues {
		if key.source != source {
			continue
		}
		h := fnv.New64a()
		var id [8]byte
		binary.LittleEndian.PutUint64(id[:], uint64(key.id))
		_, _ = h.Write(id[:])
		for _, value := range values {
			_, _ = h.Write([]byte(value))
			_, _ = h.Write([]byte{0})
		}
		// Map order is random: combine entries order-independently.
		sum += h.Sum64() | 1
	}
	return sum
}

func redactionsFingerprint(quads []TextQuad) uint64 {
	if len(quads) == 0 {
		return 0
	}
	h := fnv.New64a()
	var b [8]byte
	for _, quad := range quads {
		for _, p := range [...]float64{quad.UL.X, quad.UL.Y, quad.UR.X, quad.UR.Y, quad.LL.X, quad.LL.Y, quad.LR.X, quad.LR.Y} {
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(p))
			_, _ = h.Write(b[:])
		}
	}
	return h.Sum64() | 1
}
