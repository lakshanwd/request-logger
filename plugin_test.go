package traefikrequestlogger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	assert.NotNil(t, config)
	assert.Equal(t, "", config.Path)
	assert.Equal(t, "", config.Interval)
}

func TestNew_WithStdout(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		Path:     "",
		Interval: "1s",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)
	assert.NotNil(t, handler)

	accessLog := handler.(*RequestLogger)
	assert.Equal(t, os.Stdout, accessLog.file)
	assert.Equal(t, "1s", accessLog.interval)
}

func TestNew_WithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	ctx := context.Background()
	config := &Config{
		Path:     logPath,
		Interval: "500ms",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)
	assert.NotNil(t, handler)

	accessLog := handler.(*RequestLogger)
	assert.NotNil(t, accessLog.file)
	assert.NotEqual(t, os.Stdout, accessLog.file)
	assert.Equal(t, "500ms", accessLog.interval)

	// Cleanup
	accessLog.Close()
}

func TestNew_WithInvalidFile(t *testing.T) {
	ctx := context.Background()
	config := &Config{
		Path:     "/invalid/path/that/does/not/exist/access.log",
		Interval: "1s",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler, err := New(ctx, next, config, "test")
	assert.Error(t, err)
	assert.Nil(t, handler)
}

func TestServeHTTP(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := &Config{
		Path:     logPath,
		Interval: "100ms",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "OK", rr.Body.String())

	accessLog := handler.(*RequestLogger)
	// Wait a bit for the buffer to be populated
	time.Sleep(50 * time.Millisecond)
	assert.Greater(t, len(accessLog.buffer), 0)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify log was written
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	assert.Contains(t, logContent, "192.168.1.1:12345")
	assert.Contains(t, logContent, "GET")
	assert.Contains(t, logContent, "example.com")
	assert.Contains(t, logContent, "/test")

	accessLog.Close()
}

func TestFlush(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	accessLog := &RequestLogger{
		file:   file,
		buffer: []string{"log line 1", "log line 2", "log line 3"},
	}

	accessLog.Flush()

	// Verify buffer is cleared
	assert.Equal(t, 0, len(accessLog.buffer))

	// Verify content was written
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	assert.Contains(t, logContent, "log line 1")
	assert.Contains(t, logContent, "log line 2")
	assert.Contains(t, logContent, "log line 3")

	accessLog.Close()
}

func TestFlush_EmptyBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	accessLog := &RequestLogger{
		file:   file,
		buffer: []string{},
	}

	accessLog.Flush()

	// Verify buffer is still empty
	assert.Equal(t, 0, len(accessLog.buffer))

	// Verify file is empty or unchanged
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, "", string(content))

	accessLog.Close()
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	accessLog := &RequestLogger{
		file: file,
	}

	err = accessLog.Close()
	assert.NoError(t, err)

	// Verify file is closed by trying to write to it
	_, err = file.WriteString("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestStart_WithValidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	accessLog := &RequestLogger{
		file:     file,
		interval: "100ms",
		buffer:   []string{"test log 1", "test log 2"},
	}

	go accessLog.start(ctx)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify logs were flushed
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	assert.Contains(t, logContent, "test log 1")
	assert.Contains(t, logContent, "test log 2")

	// Verify buffer is cleared
	assert.Equal(t, 0, len(accessLog.buffer))
}

func TestStart_WithInvalidInterval(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	accessLog := &RequestLogger{
		file:     file,
		interval: "invalid-interval",
		buffer:   []string{"test log"},
	}

	go accessLog.start(ctx)

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// With invalid interval, it should flush when buffer is not empty
	// The goroutine continuously checks if buffer is not empty
	time.Sleep(100 * time.Millisecond)

	// Verify logs were flushed
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	assert.Contains(t, logContent, "test log")

	// Verify buffer is cleared
	assert.Equal(t, 0, len(accessLog.buffer))

	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestServeHTTP_LogFormat(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := &Config{
		Path:     logPath,
		Interval: "50ms",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Simulate some processing time
		w.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/users", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	// Verify log format
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	lines := strings.Split(strings.TrimSpace(logContent), "\n")
	require.Greater(t, len(lines), 0)

	logLine := lines[0]
	parts := strings.Fields(logLine)
	require.GreaterOrEqual(t, len(parts), 6)

	// Format: RemoteAddr [Timestamp] Method Host Path Duration
	assert.Equal(t, "10.0.0.1:54321", parts[0])
	assert.Contains(t, logLine, "POST")
	assert.Contains(t, logLine, "example.com")
	assert.Contains(t, logLine, "/api/users")
	assert.Contains(t, logLine, "[") // Timestamp format
	assert.Contains(t, logLine, "]")

	handler.(*RequestLogger).Close()
}

func TestMultipleRequests(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := &Config{
		Path:     logPath,
		Interval: "100ms",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)

	// Make multiple requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify all requests were logged
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	lines := strings.Split(strings.TrimSpace(logContent), "\n")
	assert.GreaterOrEqual(t, len(lines), 5)

	handler.(*RequestLogger).Close()
}

func TestConcurrentRequests(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config := &Config{
		Path:     logPath,
		Interval: "100ms",
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, config, "test")
	require.NoError(t, err)

	// Make concurrent requests
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
			req.RemoteAddr = "192.168.1.1:12345"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			done <- true
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify all requests were logged
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	logContent := string(content)
	lines := strings.Split(strings.TrimSpace(logContent), "\n")
	assert.GreaterOrEqual(t, len(lines), 10)

	handler.(*RequestLogger).Close()
}

func TestFlush_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "access.log")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	require.NoError(t, err)

	accessLog := &RequestLogger{
		file:   file,
		buffer: []string{},
	}

	// Add items concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			accessLog.buffer = append(accessLog.buffer, "log line")
			accessLog.Flush()
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no race conditions occurred
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	// Should have some logs written
	assert.Greater(t, len(content), 0)

	accessLog.Close()
}
