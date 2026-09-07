package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrNotLoopback and ErrNoRepository are the two ways Serve refuses to start.
var (
	ErrNotLoopback  = errors.New("ui_not_loopback")
	ErrNoRepository = errors.New("ui_no_repository")
)

type detailError struct {
	kind   error
	detail string
}

func (e *detailError) Error() string { return e.kind.Error() + ": " + e.detail }
func (e *detailError) Unwrap() error { return e.kind }

// IsNotLoopback reports a refused bind address; ErrorDetail returns the address.
func IsNotLoopback(err error) bool  { return errors.Is(err, ErrNotLoopback) }
func IsNoRepository(err error) bool { return errors.Is(err, ErrNoRepository) }

func ErrorDetail(err error) string {
	var detailed *detailError
	if errors.As(err, &detailed) {
		return detailed.detail
	}
	return ""
}

// pageStringKeys are the localized strings the page needs; every key must
// exist in every locale catalog, which the ui tests assert.
var pageStringKeys = []string{
	"ui.page.title", "ui.page.identity", "ui.page.repository", "ui.page.layout", "ui.page.binary",
	"ui.page.composite_identity", "ui.page.governance", "ui.page.suggestions", "ui.page.no_action", "ui.page.copy",
	"ui.page.code_index", "ui.page.objects", "ui.page.sources", "ui.page.size", "ui.page.lines", "ui.page.tokens",
	"ui.page.chunks", "ui.page.tokens_per_chunk", "ui.page.largest", "ui.page.database_index", "ui.page.evidence",
	"ui.page.bindings", "ui.page.items", "ui.page.next", "ui.page.database_unset", "ui.page.scope",
	"ui.page.observed_pending", "ui.page.recovery", "ui.page.recovery_pending", "ui.page.none",
	"ui.page.pending_transactions", "ui.page.running_servers", "ui.page.started", "ui.page.replaced_on_disk",
	"ui.page.no_running_server", "ui.page.integrations", "ui.page.integration.claude_mcp",
	"ui.page.integration.claude_hook", "ui.page.integration.codex_mcp", "ui.page.integration.opencode_mcp",
	"ui.page.integration.agents_block", "ui.page.index_header", "ui.page.entries", "ui.page.search",
	"ui.page.path", "ui.page.refreshed", "ui.page.up_to_date", "ui.page.disconnected",
}

type server struct {
	options Options
	roots   []string
	cache   *repoCache
	page    []byte
}

// Serve binds a loopback listener, calls ready with the URL, and serves until
// ctx is cancelled. It returns nil after a clean shutdown.
func Serve(ctx context.Context, options Options, ready func(url string, repositories int)) error {
	host := options.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return &detailError{kind: ErrNotLoopback, detail: host}
	}
	roots := normalizeRoots(options.Roots)
	if options.Discover {
		for _, instance := range discoverRunningServers() {
			if instance.Root != "" {
				roots = appendRoot(roots, instance.Root)
			}
		}
	}
	if len(roots) == 0 {
		return &detailError{kind: ErrNoRepository, detail: "none"}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(options.Port)))
	if err != nil {
		return err
	}
	s := &server{options: options, roots: roots, cache: newRepoCache()}
	s.page = []byte(renderPage(options.Locale))
	httpServer := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	if ready != nil {
		ready("http://"+listener.Addr().String()+"/", len(roots))
	}
	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func normalizeRoots(roots []string) []string {
	result := []string{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		result = appendRoot(result, absolute)
	}
	return result
}

func appendRoot(roots []string, candidate string) []string {
	candidate = filepath.Clean(candidate)
	for _, existing := range roots {
		if existing == candidate {
			return roots
		}
	}
	return append(roots, candidate)
}

func renderPage(locale string) string {
	strings := map[string]string{}
	for _, key := range pageStringKeys {
		strings[key] = message(locale, key)
	}
	encoded, _ := json.Marshal(strings)
	page := pageHTML
	page = replaceOnce(page, "__AOCI_STRINGS__", string(encoded))
	page = replaceOnce(page, "__AOCI_LANG__", locale)
	return page
}

func replaceOnce(text, marker, value string) string {
	index := strings.Index(text, marker)
	if index < 0 {
		return text
	}
	return text[:index] + value + text[index+len(marker):]
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.guard(s.servePage))
	mux.HandleFunc("/api/repos", s.guard(s.serveRepos))
	mux.HandleFunc("/api/state", s.guard(s.serveState))
	mux.HandleFunc("/api/entries", s.guard(s.serveEntries))
	mux.HandleFunc("/api/raw", s.guard(s.serveRaw))
	return mux
}

// guard makes every route read-only and self-contained: GET or HEAD only, no
// external resources, no caching outside the ETag handshake.
func (s *server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (s *server) servePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(s.page)
}

func (s *server) running() []Instance {
	if !s.options.Discover {
		return nil
	}
	return discoverRunningServers()
}

func (s *server) serveRepos(w http.ResponseWriter, _ *http.Request) {
	running := s.running()
	type repo struct {
		Root    string `json:"root"`
		Name    string `json:"name"`
		Running int    `json:"running"`
	}
	list := make([]repo, 0, len(s.roots))
	for _, root := range s.roots {
		entry := repo{Root: root, Name: filepath.Base(root)}
		for _, instance := range running {
			if filepath.Clean(instance.Root) == root {
				entry.Running++
			}
		}
		list = append(list, entry)
	}
	writeJSON(w, list)
}

func (s *server) lookup(w http.ResponseWriter, r *http.Request) (*cacheEntry, bool) {
	requested := r.URL.Query().Get("repo")
	root := ""
	for _, candidate := range s.roots {
		if candidate == filepath.Clean(requested) {
			root = candidate
		}
	}
	if root == "" {
		http.Error(w, "unknown repository", http.StatusNotFound)
		return nil, false
	}
	running := []Instance{}
	for _, instance := range s.running() {
		if filepath.Clean(instance.Root) == root {
			running = append(running, instance)
		}
	}
	entry, err := s.cache.get(root, s.options, running)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return entry, true
}

func (s *server) serveState(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.lookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("ETag", entry.etag)
	if r.Header.Get("If-None-Match") == entry.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(entry.body)
}

func (s *server) serveEntries(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.lookup(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	matched := filterEntries(entry.entries, query.Get("q"), query.Get("c"), query.Get("e"))
	writeJSON(w, map[string]any{"total": len(entry.entries), "matched": len(matched), "entries": matched})
}

func (s *server) serveRaw(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.lookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	switch r.URL.Query().Get("asset") {
	case "root":
		_, _ = fmt.Fprint(w, entry.snapshot.RootText)
	case "meta":
		_, _ = fmt.Fprint(w, entry.snapshot.MetaText)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
