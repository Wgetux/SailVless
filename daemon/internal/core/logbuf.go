package core

import (
	"bufio"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// LogBuf keeps the last N log lines in memory and mirrors them to stderr (journald).
type LogBuf struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewLogBuf(max int) *LogBuf { return &LogBuf{max: max} }

func (l *LogBuf) Add(prefix, line string) {
	line = strings.TrimRight(line, "\r\n")
	os.Stderr.WriteString(prefix + line + "\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, time.Now().Format("15:04:05 ")+prefix+line)
	if len(l.lines) > l.max {
		l.lines = append([]string(nil), l.lines[len(l.lines)-l.max:]...)
	}
}

// Tail returns up to n last lines (n <= 0: all).
func (l *LogBuf) Tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.lines) {
		n = len(l.lines)
	}
	return append([]string(nil), l.lines[len(l.lines)-n:]...)
}

func (l *LogBuf) consume(prefix string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l.Add(prefix, sc.Text())
	}
}
