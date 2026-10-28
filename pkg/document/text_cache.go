package document

const sourceTextCacheBytes = 16 << 20
const sourceTextCachePages = 128

type textLayoutEntry struct {
	layout TextLayout
	bytes  int
	used   uint64
}

// Native glyph geometry is disposable; source.mu protects all cache access.
// Keep this bounded independently of the GUI's word-based search cache.
type textLayoutCache struct {
	entries map[int]textLayoutEntry
	bytes   int
	clock   uint64
}

func (c *textLayoutCache) get(page int) (TextLayout, bool) {
	entry, ok := c.entries[page]
	if ok {
		c.clock++
		entry.used = c.clock
		c.entries[page] = entry
	}
	return entry.layout, ok
}

func (c *textLayoutCache) put(page int, layout TextLayout) {
	// Conservatively include glyph capacity, layout and map entry overhead.
	size := 256 + cap(layout.Glyphs)*128
	if size > sourceTextCacheBytes {
		return
	}
	if c.entries == nil {
		c.entries = make(map[int]textLayoutEntry)
	}
	c.bytes -= c.entries[page].bytes
	c.clock++
	c.entries[page] = textLayoutEntry{layout: layout, bytes: size, used: c.clock}
	c.bytes += size
	for c.bytes > sourceTextCacheBytes || len(c.entries) > sourceTextCachePages {
		oldest, selected := ^uint64(0), 0
		for page, entry := range c.entries {
			if entry.used < oldest {
				selected, oldest = page, entry.used
			}
		}
		c.bytes -= c.entries[selected].bytes
		delete(c.entries, selected)
	}
}
