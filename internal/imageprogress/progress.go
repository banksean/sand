package imageprogress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

type Update struct {
	Description    *string
	SubDescription *string
	ItemsName      *string
	AddTasks       *int64
	SetTasks       *int64
	AddTotalTasks  *int64
	SetTotalTasks  *int64
	AddItems       *int64
	SetItems       *int64
	AddTotalItems  *int64
	SetTotalItems  *int64
	AddSize        *int64
	SetSize        *int64
	AddTotalSize   *int64
	SetTotalSize   *int64
}

type Sink interface {
	io.Writer
	Update(Update)
}

type textSink struct {
	w io.Writer
}

func NewTextSink(w io.Writer) Sink {
	if w == nil {
		w = io.Discard
	}
	return textSink{w: w}
}

func (s textSink) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

func (s textSink) Update(Update) {}

type Renderer struct {
	mu          sync.Mutex
	w           io.Writer
	terminal    bool
	description string
	sub         string
	itemsName   string
	tasks       int64
	totalTasks  int64
	items       int64
	totalItems  int64
	size        int64
	totalSize   int64
	last        string
	lineOpen    bool
}

func NewRenderer(w io.Writer) *Renderer {
	if w == nil {
		w = io.Discard
	}
	renderer := &Renderer{w: w}
	if file, ok := w.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		renderer.terminal = true
	}
	return renderer
}

func (r *Renderer) Write(p []byte) (int, error) {
	r.Finish()
	return r.w.Write(p)
}

func (r *Renderer) Update(update Update) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if update.Description != nil {
		r.description = *update.Description
	}
	if update.SubDescription != nil {
		r.sub = *update.SubDescription
	}
	if update.ItemsName != nil {
		r.itemsName = *update.ItemsName
	}
	applyCounter(&r.tasks, update.AddTasks, update.SetTasks)
	applyCounter(&r.totalTasks, update.AddTotalTasks, update.SetTotalTasks)
	applyCounter(&r.items, update.AddItems, update.SetItems)
	applyCounter(&r.totalItems, update.AddTotalItems, update.SetTotalItems)
	applyCounter(&r.size, update.AddSize, update.SetSize)
	applyCounter(&r.totalSize, update.AddTotalSize, update.SetTotalSize)
	r.renderLocked()
}

func (r *Renderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal && r.lineOpen {
		fmt.Fprintln(r.w)
		r.lineOpen = false
	}
}

func (r *Renderer) renderLocked() {
	line := r.line()
	if line == "" || line == r.last {
		return
	}
	if r.terminal {
		fmt.Fprintf(r.w, "\r\033[2K%s", line)
		r.lineOpen = true
	} else {
		fmt.Fprintln(r.w, line)
	}
	r.last = line
}

func (r *Renderer) line() string {
	parts := make([]string, 0, 5)
	if r.description != "" {
		parts = append(parts, r.description)
	}
	if r.sub != "" {
		parts = append(parts, r.sub)
	}
	if r.totalTasks > 0 {
		parts = append(parts, fmt.Sprintf("tasks %d/%d", r.tasks, r.totalTasks))
	}
	if r.totalItems > 0 {
		name := r.itemsName
		if name == "" {
			name = "items"
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", name, r.items, r.totalItems))
	}
	if r.totalSize > 0 {
		parts = append(parts, fmt.Sprintf("%s/%s", formatBytes(r.size), formatBytes(r.totalSize)))
	}
	return strings.Join(parts, " ")
}

func applyCounter(value *int64, add, set *int64) {
	if set != nil {
		*value = *set
	}
	if add != nil {
		*value += *add
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
