package utils

import (
	"testing"
	"time"
)

// noopLogger is a no-op implementation of types.Logger for testing
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, fields ...interface{}) {}
func (l *noopLogger) Info(msg string, fields ...interface{})  {}
func (l *noopLogger) Warn(msg string, fields ...interface{})  {}
func (l *noopLogger) Error(msg string, fields ...interface{}) {}
func (l *noopLogger) Fatal(msg string, fields ...interface{}) {}

func TestNewCache(t *testing.T) {
	c := NewCache(5*time.Minute, &noopLogger{})
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
}

func TestCache_SetAndGetGroupName(t *testing.T) {
	c := NewCache(5*time.Minute, &noopLogger{})

	// Initially empty
	_, exists := c.GetGroupName("test-group")
	if exists {
		t.Error("expected cache miss for new key")
	}

	// Set and get
	c.SetGroupName("test-group", "Test Group Name")
	name, exists := c.GetGroupName("test-group")
	if !exists {
		t.Error("expected cache hit after set")
	}
	if name != "Test Group Name" {
		t.Errorf("expected 'Test Group Name', got '%s'", name)
	}
}

func TestCache_Expiry(t *testing.T) {
	c := NewCache(50*time.Millisecond, &noopLogger{})

	c.SetGroupName("test-group", "Test Group")
	name, exists := c.GetGroupName("test-group")
	if !exists || name != "Test Group" {
		t.Error("expected cache hit immediately after set")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	_, exists = c.GetGroupName("test-group")
	if exists {
		t.Error("expected cache miss after TTL expired")
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(5*time.Minute, &noopLogger{})

	c.SetGroupName("group1", "Group 1")
	c.SetGroupName("group2", "Group 2")

	c.Clear()

	_, exists1 := c.GetGroupName("group1")
	_, exists2 := c.GetGroupName("group2")
	if exists1 || exists2 {
		t.Error("expected empty cache after Clear()")
	}
}

func TestCache_OverwriteExistingKey(t *testing.T) {
	c := NewCache(5*time.Minute, &noopLogger{})

	c.SetGroupName("key", "value1")
	c.SetGroupName("key", "value2")

	name, exists := c.GetGroupName("key")
	if !exists {
		t.Error("expected cache hit")
	}
	if name != "value2" {
		t.Errorf("expected 'value2', got '%s'", name)
	}
}
