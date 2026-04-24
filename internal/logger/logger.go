package logger

import (
	"bufio"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Output is the shared writer used by all log sinks (Fiber middleware, slog, stdlib log).
var Output io.Writer = os.Stdout

// Init configures the global logger for the given environment and returns a Closer
// that flushes and closes any open log file.
func Init(env string) io.Closer {
	if env != "production" {
		Output = os.Stdout
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
		return io.NopCloser(nil)
	}

	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = "/logs/app.log"
	}

	rw, err := newLineRotatingWriter(logFile, 20000)
	if err != nil {
		log.Printf("Warning: cannot open log file %s (%v), logging to stdout only", logFile, err)
		Output = os.Stdout
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
		return io.NopCloser(nil)
	}

	Output = io.MultiWriter(os.Stdout, rw)
	slog.SetDefault(slog.New(slog.NewTextHandler(Output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(Output)
	log.SetFlags(0)

	return rw
}

// lineRotatingWriter writes to a file and keeps at most maxLines lines.
// When the number of new lines written since the last compaction reaches
// compactInterval, the file is rewritten keeping only the last maxLines lines.
type lineRotatingWriter struct {
	mu                sync.Mutex
	file              *os.File
	path              string
	maxLines          int
	compactInterval   int
	linesSinceCompact int
}

func newLineRotatingWriter(path string, maxLines int) (*lineRotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &lineRotatingWriter{
		file:            f,
		path:            path,
		maxLines:        maxLines,
		compactInterval: maxLines / 4,
	}, nil
}

func (w *lineRotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.file.Write(p)
	if err != nil {
		return n, err
	}

	for _, b := range p {
		if b == '\n' {
			w.linesSinceCompact++
		}
	}
	if w.linesSinceCompact >= w.compactInterval {
		w.compact()
		w.linesSinceCompact = 0
	}
	return n, nil
}

func (w *lineRotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// compact rewrites the log file keeping only the last maxLines lines.
// It writes to a temp file then atomically renames it to avoid data loss.
func (w *lineRotatingWriter) compact() {
	src, err := os.Open(w.path)
	if err != nil {
		return
	}

	var lines [][]byte
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := make([]byte, len(scanner.Bytes()))
		copy(line, scanner.Bytes())
		lines = append(lines, line)
	}
	src.Close()

	if len(lines) <= w.maxLines {
		return
	}
	lines = lines[len(lines)-w.maxLines:]

	tmpPath := w.path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}

	bw := bufio.NewWriter(tmp)
	for _, line := range lines {
		bw.Write(line)
		bw.WriteByte('\n')
	}
	bw.Flush()
	tmp.Close()

	w.file.Close()
	if err := os.Rename(tmpPath, w.path); err != nil {
		os.Remove(tmpPath)
	}

	w.file, _ = os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}
