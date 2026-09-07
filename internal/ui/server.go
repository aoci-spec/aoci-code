package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/textassets"
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
// pageStringKeys are the strings the page renders. Every official locale must
// carry all of them: the page ships both catalogs and switches without a
// reload, so a key missing from one locale would blank a live panel.
var pageStringKeys = []string{
	"ui.page.title", "ui.page.index_pane", "ui.page.status_pane", "ui.page.command_pane",
	"ui.page.env_pane", "ui.page.filter", "ui.page.no_match", "ui.page.matched_lines",
	"ui.page.asset_meta", "ui.page.volume_absent", "ui.page.copied", "ui.page.copy",
	"ui.page.tab_root", "ui.page.tab_meta", "ui.page.tab_code", "ui.page.tab_database",
	"ui.page.repository", "ui.page.governance", "ui.page.composite_identity", "ui.page.objects",
	"ui.page.sources", "ui.page.tokens", "ui.page.chunks", "ui.page.scope", "ui.page.drift",
	"ui.page.database_index", "ui.page.none", "ui.page.recovery", "ui.page.recovery_pending",
	"ui.page.no_action", "ui.page.replaced_on_disk", "ui.page.no_running_server",
	"ui.page.integration.claude_mcp", "ui.page.integration.claude_hook", "ui.page.integration.codex_mcp",
	"ui.page.integration.opencode_mcp", "ui.page.integration.agents_block",
	"ui.page.refreshed", "ui.page.up_to_date", "ui.page.disconnected",
	"ui.page.tab_all", "ui.page.copy_all", "ui.page.overview_hint", "ui.page.refresh_every",
	"ui.page.refresh_now", "ui.page.interval_10s", "ui.page.interval_30s", "ui.page.interval_1m",
	"ui.page.interval_5m", "ui.page.interval_manual", "ui.page.tokens_estimate", "ui.page.tokens_hint",
	"ui.page.stat_objects", "ui.page.stat_lines", "ui.page.stat_size", "ui.page.stat_chunks",
	"ui.page.copy_failed", "ui.page.integrations",
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

// renderPage embeds every official locale's strings and the server's active
// locale as the default. Switching language is then a client-side choice that
// needs no restart and no second request.
func renderPage(locale string) string {
	locales := []string{textassets.DefaultLocale}
	if manifest, err := textassets.ReadManifest(); err == nil && len(manifest.OfficialLocales) > 0 {
		locales = manifest.OfficialLocales
	}
	catalogs := map[string]map[string]string{}
	for _, candidate := range locales {
		bundle := map[string]string{}
		for _, key := range pageStringKeys {
			bundle[key] = message(candidate, key)
		}
		catalogs[candidate] = bundle
	}
	if _, present := catalogs[locale]; !present {
		locale = textassets.DefaultLocale
	}
	encoded, _ := json.Marshal(catalogs)
	page := replaceOnce(pageHTML, "__AOCI_I18N__", string(encoded))
	for strings.Contains(page, "__AOCI_LANG__") {
		page = replaceOnce(page, "__AOCI_LANG__", locale)
	}
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

// serveRaw returns one formal asset exactly as it is on disk. The page shows
// Volumes verbatim — section markers keep their === form — because a reader
// checking cognition needs the bytes the tools read, not a rendering of them.
func (s *server) serveRaw(w http.ResponseWriter, r *http.Request) {
	entry, ok := s.lookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	switch asset := r.URL.Query().Get("asset"); asset {
	case "root":
		_, _ = fmt.Fprint(w, entry.snapshot.RootText)
	case "meta":
		_, _ = fmt.Fprint(w, entry.snapshot.MetaText)
	case "all":
		// The complete index as an agent receives it through Overview: the
		// delivery path's own body, not a concatenation of the files.
		if entry.snapshot.OverviewText == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, entry.snapshot.OverviewText)
	case "code", "database":
		info, present := entry.snapshot.Assets[asset]
		if !present || info.State != "present" || info.Path == "" {
			http.NotFound(w, r)
			return
		}
		// The path comes from the Root manifest the loader already validated,
		// never from the request, so no query value reaches the filesystem.
		data, err := os.ReadFile(filepath.Join(entry.snapshot.Root, filepath.FromSlash(info.Path)))
		if err != nil {
			http.Error(w, "asset unavailable", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
