package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDataRoutesRequireThePageCapability(t *testing.T) {
	root := buildVolumesRepo(t)
	server := testServer(t, root)
	handler := server.handler()
	for _, path := range []string{"/api/repos", "/api/state?repo=" + root, "/api/entries?repo=" + root, "/api/raw?repo=" + root + "&asset=all"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, authorization := range []string{"", "Bearer wrong", "Bearer " + server.accessToken + "extra"} {
				request := httptest.NewRequest(method, path, nil)
				request.Header.Set("Authorization", authorization)
				request.Header.Set("If-None-Match", "*")
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusUnauthorized || strings.Contains(recorder.Body.String(), root) {
					t.Fatalf("unauthenticated data response: %d", recorder.Code)
				}
				if recorder.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("refusal may be cached")
				}
			}
		}
		if response := get(t, handler, path, nil); response.Code != http.StatusOK {
			t.Fatalf("authenticated route failed: %s: %d", path, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), server.accessToken) || strings.Contains(recorder.Body.String(), root) {
		t.Fatal("static shell exposed protected data")
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("page can be framed")
	}
	server.accessToken = ""
	if response := get(t, handler, "/api/repos", nil); response.Code != http.StatusUnauthorized {
		t.Fatal("empty server credential must fail closed")
	}
}

func TestPageRegistryAuthenticationAndLegacyStopCompatibility(t *testing.T) {
	root := "/synthetic/repository"
	var calls int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	reg := Registration{URL: origin.URL + "/#access_token=" + strings.Repeat("b", 64), Root: root}
	if Live(reg, time.Second) || calls != 0 {
		t.Fatal("liveness probe followed a redirect")
	}
	if !HasAccessToken(reg.URL) || HasAccessToken(origin.URL) {
		t.Fatal("new detach must distinguish legacy pages")
	}
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos" {
			t.Fatal("incorrect legacy probe")
		}
		_, _ = w.Write([]byte(`[{"root":"/synthetic/repository"}]`))
	}))
	defer legacy.Close()
	if !Live(Registration{URL: legacy.URL, Root: root}, time.Second) {
		t.Fatal("legacy registration must remain stoppable")
	}
	for _, raw := range []string{"https://127.0.0.1/", "http://example.test/", "http://user:secret@127.0.0.1/", "http://127.0.0.1/?token=secret", origin.URL + "/#access_token=wrong"} {
		if _, err := pageRequest(raw, "/api/repos"); err == nil {
			t.Fatal("unsafe registry URL accepted")
		}
	}
	dir := t.TempDir()
	if err := Register(dir, reg); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(registrationPath(dir, root))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("credential-bearing registry must be private")
		}
	}
}

func TestRegistrationReplacementTightensExistingPermissions(t *testing.T) {
	dir := t.TempDir()
	previous := Registration{PID: 10, Root: "/synthetic/repository", URL: "http://127.0.0.1:12345/"}
	if err := Register(dir, previous); err != nil {
		t.Fatal(err)
	}
	path := registrationPath(dir, previous.Root)
	// Simulate a registration written with broader permissions before the
	// capability was introduced. Reusing its pathname must not inherit them.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	replacement := Registration{PID: 20, Root: previous.Root,
		URL: "http://127.0.0.1:12346/#access_token=" + strings.Repeat("c", 64)}
	if err := Register(dir, replacement); err != nil {
		t.Fatal(err)
	}
	got, ok := Registered(dir, replacement.Root)
	if !ok || got.PID != replacement.PID || got.URL != replacement.URL || !HasAccessToken(got.URL) {
		t.Fatal("replacement registration lost its process or capability binding")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("replacement inherited public permissions: %v", info.Mode().Perm())
		}
	}
}

func TestPageSendsCapabilityOnlyInHeaders(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for page JavaScript regression tests")
	}
	cmd := exec.Command(node, "-e", `
const assert=require('node:assert/strict'),vm=require('node:vm');
const html=require('node:fs').readFileSync(0,'utf8');
let source=html.split('<script>')[1].split('</script>')[0];
source=source.replace('(async () => { renderChrome(); await loadRepos(); renderChrome(); await refresh(true); schedule(); })();','globalThis.apiFetch=apiFetch;');
const calls=[],saved=new Map();let cleared=false;
const element={addEventListener(){},querySelector(){return null;}};
const context=vm.createContext({URLSearchParams,
 window:{addEventListener(){},location:{hash:'#access_token=synthetic-token',pathname:'/',search:''},history:{replaceState(a,b,c){assert.equal(c,'/');cleared=true;}}},
 sessionStorage:{getItem:k=>saved.get(k),setItem:(k,v)=>saved.set(k,v)},
 document:{documentElement:{},getElementById:()=>element},
 fetch:(url,options)=>{calls.push({url,options});return Promise.resolve({});},setTimeout(){},clearTimeout(){}
});
vm.runInContext(source,context);
context.apiFetch('/api/repos',{headers:{'If-None-Match':'version'}});
assert.equal(calls[0].url,'/api/repos');
assert.equal(calls[0].options.headers.Authorization,'Bearer synthetic-token');
assert.equal(calls[0].options.headers['If-None-Match'],'version');
assert.equal(calls[0].options.redirect,'error');
assert.equal(saved.get('aoci.accessToken'),'synthetic-token');assert.ok(cleared);
`)
	cmd.Stdin = strings.NewReader(pageHTML)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("page authentication regression: %v\n%s", err, output)
	}
}
