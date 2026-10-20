package server

import (
	"sort"
	"sync"
	"time"
)

// TemplateStat holds the timing statistics of one template.
type TemplateStat struct {
	Path    string  `json:"path"`
	Count   int64   `json:"count"`
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	TotalMs int64   `json:"totalMs"`
	AvgMs   float64 `json:"avgMs"`
	MaxMs   int64   `json:"maxMs"`
	LastMs  int64   `json:"lastMs"`
	Errors  int64   `json:"errors"`
	// HitRate is the cache hit rate, 0..1.
	HitRate float64 `json:"hitRate"`
}

// PerfStats collects per-template rendering statistics so the slow reports
// can be spotted.
type PerfStats struct {
	mu    sync.Mutex
	stats map[string]*TemplateStat
	limit int
}

// NewPerfStats creates a collector. The limit caps how many templates are
// tracked so a long-running instance cannot grow without bound.
func NewPerfStats(limit int) *PerfStats {
	if limit <= 0 {
		limit = 200
	}
	return &PerfStats{stats: map[string]*TemplateStat{}, limit: limit}
}

// Observe records one render of a template.
func (p *PerfStats) Observe(path string, elapsed time.Duration, cached bool, failed bool) {
	if p == nil || path == "" {
		return
	}
	ms := elapsed.Milliseconds()
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.stats[path]
	if s == nil {
		if len(p.stats) >= p.limit {
			// Drop the least recently used entry to make room.
			var oldestKey string
			var oldest int64
			for k, v := range p.stats {
				if oldestKey == "" || v.Count < oldest {
					oldestKey = k
					oldest = v.Count
				}
			}
			if oldestKey != "" {
				delete(p.stats, oldestKey)
			}
		}
		s = &TemplateStat{Path: path}
		p.stats[path] = s
	}
	s.Count++
	s.TotalMs += ms
	s.LastMs = ms
	if ms > s.MaxMs {
		s.MaxMs = ms
	}
	if cached {
		s.Hits++
	} else {
		s.Misses++
	}
	if failed {
		s.Errors++
	}
	if s.Count > 0 {
		s.AvgMs = float64(s.TotalMs) / float64(s.Count)
	}
	if s.Hits+s.Misses > 0 {
		s.HitRate = float64(s.Hits) / float64(s.Hits+s.Misses)
	}
}

// Snapshot returns the statistics sorted by total time spent, slowest first.
func (p *PerfStats) Snapshot() []TemplateStat {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	out := make([]TemplateStat, 0, len(p.stats))
	for _, s := range p.stats {
		out = append(out, *s)
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMs != out[j].TotalMs {
			return out[i].TotalMs > out[j].TotalMs
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Total returns the number of templates being tracked.
func (p *PerfStats) Total() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.stats)
}
