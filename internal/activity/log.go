// Package activity writes the daemon's metadata-only activity journal.
package activity

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Event intentionally has no free-form message, error or protocol payload.
type Event struct {
	RetrySeconds int       `json:"retry_seconds,omitempty"`
	Time         time.Time `json:"time"`
	Name         string    `json:"event"`
	Connection   string    `json:"connection_id,omitempty"`
	Device       string    `json:"device,omitempty"`
	Address      string    `json:"address,omitempty"`
	Role         string    `json:"role,omitempty"`
	Supervisor   string    `json:"supervisor_id,omitempty"`
	Session      string    `json:"session_id,omitempty"`
	Stream       *int64    `json:"stream_id,omitempty"`
	Operation    string    `json:"operation,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	Duration     *int64    `json:"duration_ms,omitempty"`
}

func Since(start time.Time) *int64 {
	ms := time.Since(start).Milliseconds()
	return &ms
}

type Log struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	display     func(Event, []byte)
	failure     func(error)
	size, limit int64
}

func Open(path string, stderr io.Writer) *Log {
	return OpenJournal(path, path == "", func(_ Event, data []byte) { stderr.Write(data) }, func(err error) {
		fmt.Fprintf(stderr, "clistreamd: activity log unavailable (%v), writing events to stderr\n", err)
	})
}

// OpenJournal writes a private rotating journal and presents each serialized event.
func OpenJournal(path string, noFile bool, display func(Event, []byte), failure func(error)) *Log {
	l := &Log{path: path, display: display, failure: failure, limit: 10 << 20}
	if !noFile {
		if err := l.open(); err != nil {
			l.fallback(err)
		}
	}
	return l
}

func (l *Log) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1) {
		err = fmt.Errorf("expected a regular single-link log file")
	}
	if err == nil {
		err = f.Chmod(0600)
	}
	if err != nil {
		f.Close()
		return err
	}
	l.file, l.size = f, info.Size()
	return nil
}

func (l *Log) rotate() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	l.file = nil
	for i := 5; i >= 1; i-- {
		source := l.path
		if i > 1 {
			source += "." + strconv.Itoa(i-1)
		}
		err := os.Rename(source, l.path+"."+strconv.Itoa(i))
		if err != nil && !(i > 1 && os.IsNotExist(err)) {
			return err
		}
	}
	return l.open()
}

func (l *Log) fallback(err error) {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	l.failure(err)
}

func (l *Log) Record(event Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	event.Time = time.Now().UTC()
	data, _ := json.Marshal(event)
	data = append(data, '\n')
	if l.file != nil && l.size > 0 && l.size+int64(len(data)) > l.limit {
		if err := l.rotate(); err != nil {
			l.fallback(err)
		}
	}
	if l.file != nil {
		n, err := l.file.Write(data)
		if err == nil {
			l.size += int64(n)
		} else {
			// Remove an incomplete final record if the filesystem still permits it.
			if n > 0 {
				_ = l.file.Truncate(l.size)
			}
			l.fallback(err)
		}
	}
	l.display(event, data)
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}
