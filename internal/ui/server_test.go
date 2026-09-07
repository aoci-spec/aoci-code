package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// buildVolumesRepo mirrors the mcptools test fixture: a Volumes v1 repository
// with one Code object, scanned into a Baseline, so Assess reports aligned.
func buildVolumesRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: ui fixture\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n"+
		"#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\nmain.go[CD9S]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep execution deterministic\n")
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func testServer(t *testing.T, roots ...string) *server {
	t.Helper()
	options := Options{Roots: roots, Locale: "en-US", BinaryVersion: "test"}
	return &server{options: options, roots: normalizeRoots(roots), cache: newRepoCache(), page: []byte(renderPage("en-US"))}
}

func get(t *testing.T, handler http.Handler, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestStateReportsTheSharedGovernanceFacts(t *testing.T) {
	root := buildVolumesRepo(t)
	handler := testServer(t, root).handler()
	response := get(t, handler, "/api/state?repo="+root, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var snapshot Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Error != "" || snapshot.Facts == nil {
		t.Fatalf("snapshot not built: %+v", snapshot)
	}
	if snapshot.Layout != cognition.LayoutVolumesV1 || snapshot.Facts.CodeEntryCount != 1 || snapshot.Assets["code"].Objects != 1 {
		t.Fatalf("unexpected facts: layout=%s entries=%d assets=%+v", snapshot.Layout, snapshot.Facts.CodeEntryCount, snapshot.Assets)
	}
	if snapshot.ChunkPlan == nil || snapshot.ChunkPlan.ChunkCount < 1 || snapshot.ChunkPlan.EntryCount != 1 {
		t.Fatalf("chunk plan missing: %+v", snapshot.ChunkPlan)
	}
	if !strings.Contains(snapshot.RootText, cognition.RootManifestMarker) || !strings.Contains(snapshot.MetaText, cognition.MetaVolumeMarker) {
		t.Fatalf("header texts missing")
	}
	for _, locale := range []string{"en-US", "zh-CN"} {
		if len(snapshot.Suggestions[locale]) == 0 {
			t.Fatalf("no %s suggestions", locale)
		}
	}
	if snapshot.Suggestions["en-US"][0].Label == snapshot.Suggestions["zh-CN"][0].Label {
		t.Fatalf("suggestions are not localized per locale")
	}
	// Coverage is measured from the indexed sources on disk, never guessed.
	if c := snapshot.Coverage; c == nil || c.Files == 0 || c.Lines == 0 || c.Unreadable != 0 || c.EstimatedTokens != c.Bytes/3 || c.IndexTokens == 0 {
		t.Fatalf("coverage not measured: %+v", snapshot.Coverage)
	}
	for _, key := range []string{"claude_mcp", "claude_hook", "codex_mcp", "opencode_mcp", "agents_block"} {
		if _, present := snapshot.Integrations[key]; !present {
			t.Fatalf("integration %s missing", key)
		}
	}
}

func TestStateHonoursETagAndStaysReadOnly(t *testing.T) {
	root := buildVolumesRepo(t)
	handler := testServer(t, root).handler()
	first := get(t, handler, "/api/state?repo="+root, nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if again := get(t, handler, "/api/state?repo="+root, map[string]string{"If-None-Match": etag}); again.Code != http.StatusNotModified {
		t.Fatalf("unchanged repository answered %d, want 304", again.Code)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), append([]byte{}, []byte(strings.Replace(readFile(t, root, "aoci.code.txt"), "run the fixture", "run the changed fixture", 1))...), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed := get(t, handler, "/api/state?repo="+root, map[string]string{"If-None-Match": etag}); changed.Code != http.StatusOK || changed.Header().Get("ETag") == etag {
		t.Fatalf("a changed Volume must produce a new snapshot: %d %s", changed.Code, changed.Header().Get("ETag"))
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/api/state?repo="+root, strings.NewReader("{}"))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s answered %d, want 405", method, recorder.Code)
		}
	}
	if unknown := get(t, handler, "/api/state?repo=/nowhere", nil); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown repository answered %d", unknown.Code)
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "ledger.jsonl")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the page must not create audit files: %v", err)
	}
}

func TestEntriesRawAndPage(t *testing.T) {
	root := buildVolumesRepo(t)
	handler := testServer(t, root).handler()
	var entries struct {
		Total   int         `json:"total"`
		Matched int         `json:"matched"`
		Entries []EntryView `json:"entries"`
	}
	if err := json.Unmarshal(get(t, handler, "/api/entries?repo="+root+"&q=fixture", nil).Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if entries.Total != 1 || entries.Matched != 1 || entries.Entries[0].Path != "main.go" || entries.Entries[0].C != "9" || entries.Entries[0].E != "S" {
		t.Fatalf("entries: %+v", entries)
	}
	if err := json.Unmarshal(get(t, handler, "/api/entries?repo="+root+"&c=3", nil).Body.Bytes(), &entries); err != nil || entries.Matched != 0 {
		t.Fatalf("C filter: %v %+v", err, entries)
	}
	for _, probe := range []struct{ asset, marker string }{
		{"root", cognition.RootManifestMarker}, {"meta", cognition.MetaVolumeMarker}, {"code", cognition.CodeVolumeMarker},
	} {
		raw := get(t, handler, "/api/raw?repo="+root+"&asset="+probe.asset, nil)
		if !strings.HasPrefix(raw.Body.String(), probe.marker) {
			t.Fatalf("raw %s did not start with its marker: %q", probe.asset, raw.Body.String()[:60])
		}
	}
	// The Code Volume must arrive byte for byte, section markers included: the
	// page shows what the tools read, not a rendering of it.
	onDisk := readFile(t, root, "aoci.code.txt")
	if served := get(t, handler, "/api/raw?repo="+root+"&asset=code", nil).Body.String(); served != onDisk {
		t.Fatalf("served Code Volume is not the file on disk")
	}
	if !strings.Contains(onDisk, "\n===") {
		t.Fatalf("fixture lost its === section marker")
	}
	if absent := get(t, handler, "/api/raw?repo="+root+"&asset=database", nil); absent.Code != http.StatusNotFound {
		t.Fatalf("absent Volume answered %d", absent.Code)
	}
	// "all" is the Overview body itself: every Volume behind its asset banner,
	// so the page's complete index is what an agent receives, not a rendering.
	all := get(t, handler, "/api/raw?repo="+root+"&asset=all", nil).Body.String()
	if !strings.Contains(all, "AOCI Cognition Asset: id=code ") || !strings.Contains(all, onDisk) {
		t.Fatalf("all did not carry the Code Volume behind its banner")
	}
	if strings.Index(all, "id=root ") > strings.Index(all, "id=code ") {
		t.Fatalf("all is not in delivery order")
	}
	page := get(t, handler, "/", nil).Body.String()
	// The page ships every official locale and switches without a reload, so
	// both catalogs must be present and no placeholder may survive rendering.
	if !strings.Contains(page, "window.AOCI_I18N = {") || strings.Contains(page, "__AOCI_") || strings.Contains(page, "https://") {
		t.Fatalf("page is not self-contained")
	}
	for _, locale := range []string{"en-US", "zh-CN"} {
		title, err := textassets.Message(locale, "ui.page.index_pane")
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		if !strings.Contains(page, `"`+locale+`":`) || !strings.Contains(page, title) {
			t.Fatalf("page does not carry the %s catalog", locale)
		}
	}
	if !strings.Contains(page, `window.AOCI_LOCALE = "en-US"`) {
		t.Fatalf("page does not default to the server locale")
	}
	if nested := get(t, handler, "/anything", nil); nested.Code != http.StatusNotFound {
		t.Fatalf("unknown path answered %d", nested.Code)
	}
}

func TestPageStringsResolveInEveryOfficialLocale(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		keys := append([]string{}, pageStringKeys...)
		keys = append(keys, "ui.suggest.build_index", "ui.suggest.database", "ui.suggest.recall", "ui.suggest.blocked",
			"ui.suggest.evidence", "ui.suggest.recovery", "ui.suggest.batch", "ui.prompt.build_index", "ui.prompt.database", "ui.prompt.recall")
		if err := textassets.ValidateMessageKeys(locale, keys); err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
	}
}

func TestServeRefusesNonLoopbackAndEmptyRepositorySets(t *testing.T) {
	err := Serve(context.Background(), Options{Roots: []string{t.TempDir()}, Host: "0.0.0.0"}, nil)
	if !IsNotLoopback(err) || ErrorDetail(err) != "0.0.0.0" {
		t.Fatalf("non-loopback bind accepted: %v", err)
	}
	if err := Serve(context.Background(), Options{Host: "127.0.0.1"}, nil); !IsNoRepository(err) {
		t.Fatalf("empty repository set accepted: %v", err)
	}
}

func TestServeListensOnLoopbackAndStopsWithContext(t *testing.T) {
	root := buildVolumesRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	urls := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Roots: []string{root}, Host: "127.0.0.1", Locale: "en-US"}, func(url string, repositories int) {
			if repositories != 1 {
				t.Errorf("repositories=%d", repositories)
			}
			urls <- url
		})
	}()
	select {
	case url := <-urls:
		if !strings.HasPrefix(url, "http://127.0.0.1:") {
			t.Fatalf("url %q", url)
		}
		response, err := http.Get(url + "api/repos")
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("repos: %v %v", err, response)
		}
		response.Body.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("server did not become ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestServingChangesNothingOnDisk is the machine form of the claim the page
// makes about itself: it reads the repository the way the read-only CLI
// commands do and leaves every byte where it was. A page that quietly touched
// a formal asset, advanced a Baseline, or appended an audit event could
// disturb a governance cycle running beside it in another process.
func TestServingChangesNothingOnDisk(t *testing.T) {
	root := buildVolumesRepo(t)
	before := treeDigest(t, root)
	handler := testServer(t, root).handler()
	for round := 0; round < 5; round++ {
		for _, target := range []string{
			"/", "/api/repos", "/api/state?repo=" + root, "/api/entries?repo=" + root,
			"/api/entries?repo=" + root + "&q=fixture&c=9&e=S",
			"/api/raw?repo=" + root + "&asset=root", "/api/raw?repo=" + root + "&asset=meta",
		} {
			if response := get(t, handler, target, nil); response.Code != http.StatusOK && response.Code != http.StatusNotModified {
				t.Fatalf("%s answered %d", target, response.Code)
			}
		}
	}
	after := treeDigest(t, root)
	if len(before) != len(after) {
		t.Fatalf("file set changed: %d entries before, %d after", len(before), len(after))
	}
	for path, digest := range before {
		if after[path] != digest {
			t.Fatalf("%s changed while only serving the page", path)
		}
	}
}

// treeDigest maps every repository-relative path to its content digest, so a
// changed byte, a new audit line, or a fresh file all show up as a difference.
func treeDigest(t *testing.T, root string) map[string]string {
	t.Helper()
	digests := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		digests[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return digests
}
