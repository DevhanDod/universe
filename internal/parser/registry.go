package parser

import "sync"

// Registry holds all registered parsers, keyed by file extension
type Registry struct {
	mu      sync.RWMutex
	parsers map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{
		parsers: make(map[string]Parser),
	}
}

// Register adds a parser for its supported extensions
func (r *Registry) Register(p Parser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ext := range p.SupportedExtensions() {
		r.parsers[ext] = p
	}
}

// GetParser returns the parser for a given file extension, or nil if unsupported
func (r *Registry) GetParser(extension string) Parser {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.parsers[extension]
}

// SupportedExtensions returns all extensions the registry can handle
func (r *Registry) SupportedExtensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exts := make([]string, 0, len(r.parsers))
	for ext := range r.parsers {
		exts = append(exts, ext)
	}
	return exts
}
