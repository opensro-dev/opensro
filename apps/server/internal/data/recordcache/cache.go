/*
===========================================================================

cache.go - immutable process-local catalogue backing files

A Cache keeps verified catalogue records in an anonymous temp file and
only a bounded LRU of decoded rows in memory. The file belongs to the
process alone: on Linux it is unlinked the moment it is created, so the
kernel frees it when the process ends, however it ends. Production once
kept every GameWorld start's files in /tmp (200-420 MB each) because only
Close removed them, and a killed or restarted process never closes.

===========================================================================
*/
package recordcache

import (
	"container/list"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
)

type location struct {
	offset int64
	size   int
}
type entry[T any] struct {
	id    uint32
	value T
}
type Cache[T any] struct {
	mu   sync.Mutex
	file *os.File
	// name is the backing file's path while it still has one: only where
	// the OS refuses to unlink an open file (Windows). Close removes it.
	name  string
	next  int64
	limit int
	index map[uint32]location
	pages map[uint32]*list.Element
	lru   list.List
}

func New[T any](capacity int) (*Cache[T], error) {
	if capacity < 1 {
		return nil, fmt.Errorf("invalid record cache capacity")
	}
	f, err := os.CreateTemp("", "sro-catalogue-*.bin")
	if err != nil {
		return nil, err
	}
	c := &Cache[T]{file: f, limit: capacity, index: make(map[uint32]location), pages: make(map[uint32]*list.Element)}
	// The open descriptor keeps the bytes after the unlink. Windows refuses
	// to remove an open file; there the name stays until Close.
	if os.Remove(f.Name()) != nil {
		c.name = f.Name()
	}
	return c, nil
}

// Put runs only before publication. Verify semantic round trips before dropping
// source rows, so adding an unserializable/private field cannot lose game data.
func (c *Cache[T]) Put(id uint32, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var check T
	if err = json.Unmarshal(data, &check); err != nil {
		return err
	}
	if !reflect.DeepEqual(value, check) {
		return fmt.Errorf("catalogue record %d does not round-trip", id)
	}
	if len(data) > 128<<10 {
		return fmt.Errorf("catalogue record %d too large", id)
	}
	if n, err := c.file.WriteAt(data, c.next); err != nil {
		return err
	} else if n != len(data) {
		return fmt.Errorf("short catalogue write")
	}
	c.index[id] = location{c.next, len(data)}
	c.next += int64(len(data))
	return nil
}

func (c *Cache[T]) Get(id uint32) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.pages[id]; e != nil {
		c.lru.MoveToFront(e)
		return e.Value.(entry[T]).value, true
	}
	p, ok := c.index[id]
	if !ok {
		var zero T
		return zero, false
	}
	data := make([]byte, p.size)
	if _, err := c.file.ReadAt(data, p.offset); err != nil {
		panic(fmt.Sprintf("catalogue read failed: %v", err))
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		panic(fmt.Sprintf("catalogue decode failed: %v", err))
	}
	if len(c.pages) == c.limit {
		e := c.lru.Back()
		delete(c.pages, e.Value.(entry[T]).id)
		c.lru.Remove(e)
	}
	c.pages[id] = c.lru.PushFront(entry[T]{id, value})
	return value, true
}
func (c *Cache[T]) Len() int { return len(c.index) }
func (c *Cache[T]) IDs() []uint32 {
	ids := make([]uint32, 0, len(c.index))
	for id := range c.index {
		ids = append(ids, id)
	}
	return ids
}
func (c *Cache[T]) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return nil
	}
	f := c.file
	c.file = nil
	c.pages = nil
	c.lru.Init()
	err := f.Close()
	if c.name != "" {
		if removeErr := os.Remove(c.name); err == nil {
			err = removeErr
		}
		c.name = ""
	}
	return err
}
