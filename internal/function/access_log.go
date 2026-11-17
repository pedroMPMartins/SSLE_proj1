package function

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AccessLogger manages thread-safe Apache Combined format access logging to a file for Wazuh SIEM.
type AccessLogger struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// NewAccessLogger initializes an AccessLogger for the given service kind and optional explicit path.
func NewAccessLogger(serviceKind, explicitPath string) *AccessLogger {
	logPath := explicitPath
	if logPath == "" {
		logPath = os.Getenv("ACCESS_LOG_PATH")
	}
	if logPath == "" {
		logPath = fmt.Sprintf("/var/log/ssle/%s_access.log", serviceKind)
	}

	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logPath = fmt.Sprintf("./%s_access.log", serviceKind)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logPath = fmt.Sprintf("./%s_access.log", serviceKind)
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("[Wazuh SIEM] Warning: Unable to open access log for %s: %v. File logging disabled.\n", serviceKind, err)
			return &AccessLogger{path: logPath, file: nil}
		}
	}

	log.Printf("[Wazuh SIEM] Structured access logging for '%s' initialized at: %s\n", serviceKind, logPath)
	return &AccessLogger{
		path: logPath,
		file: f,
	}
}

// Log writes standard Apache Combined format entry for Wazuh localfile ingestion.
// Format: %h - - [%t] "%r" %>s %b "-" "%{User-Agent}"
func (l *AccessLogger) Log(clientIP, method, uri, proto string, status, bytesSent int, userAgent string) {
	if l == nil || l.file == nil {
		return
	}

	timeStr := time.Now().Format("02/Jan/2006:15:04:05 -0700")
	line := fmt.Sprintf("%s - - [%s] \"%s %s %s\" %d %d \"-\" \"%s\"\n",
		clientIP, timeStr, method, uri, proto, status, bytesSent, userAgent)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.file.WriteString(line)
}

// Path returns the active file path for the access log.
func (l *AccessLogger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close flushes and closes the underlying log file.
func (l *AccessLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

type statusRecorder struct {
	http.ResponseWriter
	status    int
	bytesSent int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesSent += n
	return n, err
}

// AccessLoggingMiddleware wraps an http.Handler with Apache-compatible access logging.
func AccessLoggingMiddleware(logger *AccessLogger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		clientIP := ExtractClientIP(r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		uri := r.RequestURI
		if uri == "" {
			if r.URL != nil {
				uri = r.URL.RequestURI()
				if uri == "" {
					uri = r.URL.Path
				}
			}
			if uri == "" {
				uri = "/"
			}
		}
		if logger != nil {
			logger.Log(clientIP, r.Method, uri, r.Proto, status, rec.bytesSent, r.UserAgent())
		}
	})
}
