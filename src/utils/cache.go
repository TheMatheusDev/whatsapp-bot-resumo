package utils

import (
	"sync"
	"time"

	"whatsapp-summarizer/src/types"
)

// Cache implements the CacheService interface
type Cache struct {
	mu     sync.RWMutex
	data   map[string]types.GroupInfo
	ttl    time.Duration
	logger types.Logger
}

// NewCache creates a new cache service
func NewCache(ttl time.Duration, logger types.Logger) *Cache {
	return &Cache{
		data:   make(map[string]types.GroupInfo),
		ttl:    ttl,
		logger: logger,
	}
}

// GetGroupName gets a cached group name
func (c *Cache) GetGroupName(chatID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, exists := c.data[chatID]
	if !exists {
		return "", false
	}

	// Check if cache entry is expired
	if time.Since(info.CachedAt) > c.ttl {
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.data, chatID)
		c.mu.Unlock()
		c.mu.RLock()
		return "", false
	}

	return info.Name, true
}

// SetGroupName sets a group name in cache
func (c *Cache) SetGroupName(chatID, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[chatID] = types.GroupInfo{
		Name:     name,
		CachedAt: time.Now(),
	}

	c.logger.Debug("Cached group name", "chat_id", chatID, "|", name)
}

// Clear clears all cache entries
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]types.GroupInfo)
	c.logger.Info("Cache cleared")
}
