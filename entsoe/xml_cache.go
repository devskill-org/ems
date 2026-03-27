// Package entsoe provides a client for the ENTSO-E Transparency Platform API.
package entsoe

import (
	"bytes"
	"fmt"
	"sync"
	"time"
)

// CacheSource identifies how a document was added to the XMLDocumentCache.
type CacheSource string

const (
	// CacheSourceUpload means the document was uploaded manually via the web interface.
	CacheSourceUpload CacheSource = "upload"

	// CacheSourceDownload means the document was fetched automatically from the ENTSO-E API.
	CacheSourceDownload CacheSource = "download"
)

// CacheEntry holds a cached PublicationMarketData document with metadata.
type CacheEntry struct {
	// Document is the parsed market data.
	Document *PublicationMarketData

	// UploadedAt is the time the document was stored in the cache.
	UploadedAt time.Time

	// Date is the date key this entry was stored under (YYYY-MM-DD).
	Date string

	// Source identifies whether the entry came from a manual upload or an automatic download.
	Source CacheSource
}

// XMLDocumentCache caches parsed PublicationMarketData by date string (YYYY-MM-DD).
// It is safe for concurrent use from multiple goroutines.
type XMLDocumentCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
}

// NewXMLDocumentCache creates a new, empty XMLDocumentCache.
func NewXMLDocumentCache() *XMLDocumentCache {
	return &XMLDocumentCache{
		entries: make(map[string]*CacheEntry),
	}
}

// Store parses xmlData as a PublicationMarketData XML document and stores the
// result under the given date key (expected format: YYYY-MM-DD) with
// CacheSourceUpload as the source.
// Returns an error if the XML cannot be parsed.
func (c *XMLDocumentCache) Store(date string, xmlData []byte) error {
	doc, err := DecodeEnergyPricesXML(bytes.NewReader(xmlData))
	if err != nil {
		return fmt.Errorf("failed to parse XML for date %s: %w", date, err)
	}

	c.StoreDocument(date, doc, CacheSourceUpload)
	return nil
}

// StoreDocument stores an already-parsed PublicationMarketData document under
// the given date key (expected format: YYYY-MM-DD).
// Any existing entry for that date is overwritten.
func (c *XMLDocumentCache) StoreDocument(date string, doc *PublicationMarketData, source CacheSource) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[date] = &CacheEntry{
		Document:   doc,
		UploadedAt: time.Now(),
		Date:       date,
		Source:     source,
	}
}

// Get returns the cached PublicationMarketData for the given date key.
// Returns (nil, false) when no entry exists for that date.
func (c *XMLDocumentCache) Get(date string) (*PublicationMarketData, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[date]
	if !ok {
		return nil, false
	}
	return entry.Document, true
}

// Delete removes the cached entry for the given date key.
// It is a no-op if no entry exists for that date.
func (c *XMLDocumentCache) Delete(date string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, date)
}

// Cleanup removes all entries whose date key is strictly less than cutoffDate.
// cutoffDate must be in YYYY-MM-DD format. Because that format sorts
// lexicographically in chronological order, a simple string comparison is used.
// Returns the number of entries removed.
func (c *XMLDocumentCache) Cleanup(cutoffDate string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for date := range c.entries {
		if date < cutoffDate {
			delete(c.entries, date)
			removed++
		}
	}
	return removed
}

// ListEntries returns a snapshot of all cached entries.
// The returned slice is a copy; modifications to it do not affect the cache.
func (c *XMLDocumentCache) ListEntries() []CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := make([]CacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, *e)
	}
	return entries
}

// Len returns the number of entries currently held in the cache.
func (c *XMLDocumentCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}