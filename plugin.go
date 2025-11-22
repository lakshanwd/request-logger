package traefikrequestlogger

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Config the plugin configuration.
type Config struct {
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{}
}

type RequestLogger struct {
	mu       sync.Mutex
	file     *os.File
	next     http.Handler
	buffer   []string
	interval string
}

func (e *RequestLogger) start(ctx context.Context) {
	defer e.Close()
	defer e.Flush()

	// parse the interval
	interval, err := time.ParseDuration(e.interval)

	// interval is not valid, flush the buffer every time the buffer is not empty
	if err != nil {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if len(e.buffer) > 0 {
					e.Flush()
				}
			}
		}
	}

	// interval is valid, flush the buffer every interval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Flush()
		}
	}
}

// New creates a new AccessLog plugin.
func New(ctx context.Context, next http.Handler, config *Config, _ string) (http.Handler, error) {
	var file *os.File
	if config.Path == "" {
		file = os.Stdout
	} else {
		var err error
		file, err = os.OpenFile(config.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
	}
	instance := RequestLogger{
		file:     file,
		next:     next,
		interval: config.Interval,
	}

	go instance.start(ctx)
	return &instance, nil
}

func (e *RequestLogger) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	start := time.Now()
	defer func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.buffer = append(e.buffer, fmt.Sprintf("%s [%s] %s %s %s %s", req.RemoteAddr, start.Format(time.RFC3339), req.Method, req.Host, req.URL.Path, time.Since(start).String()))
	}()
	e.next.ServeHTTP(rw, req)
}

func (e *RequestLogger) Flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, line := range e.buffer {
		e.file.WriteString(line + "\n")
	}
	e.buffer = nil
}

func (e *RequestLogger) Close() error {
	return e.file.Close()
}
