package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/fs"
)

func TestRegistryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	reg := Registration{PID: 4242, URL: "http://127.0.0.1:1/", Root: "/repo/a", StartedAt: time.Now().UTC()}
	if err := Register(dir, reg); err != nil {
		t.Fatal(err)
	}
	got, ok := Registered(dir, "/repo/a")
	if !ok || got.PID != 4242 || got.URL != reg.URL || got.Root != "/repo/a" || got.Version != registryVersion {
		t.Fatalf("registered: %+v %v", got, ok)
	}
	if _, ok := Registered(dir, "/repo/b"); ok {
		t.Fatalf("a foreign root resolved to this registration")
	}
	// A page shutting down must not remove a newer page's record.
	if err := Unregister(dir, "/repo/a", 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := Registered(dir, "/repo/a"); !ok {
		t.Fatalf("another process's registration was removed")
	}
	if err := Unregister(dir, "/repo/a", 4242); err != nil {
		t.Fatal(err)
	}
	if _, ok := Registered(dir, "/repo/a"); ok {
		t.Fatalf("registration survived its owner's unregister")
	}
}

func TestServeRegistersAndUnregisters(t *testing.T) {
	root := buildVolumesRepo(t)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Roots: []string{root}, Host: "127.0.0.1", Locale: "en-US", RegistryDir: dir}, func(url string, _ int) { ready <- url })
	}()
	var url string
	select {
	case url = <-ready:
	case err := <-done:
		t.Fatalf("serve ended: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("page never reported readiness")
	}
	reg, ok := Registered(dir, root)
	if !ok || reg.URL != url || reg.PID != os.Getpid() {
		t.Fatalf("page did not register itself: %+v", reg)
	}
	if !Live(reg, 2*time.Second) {
		t.Fatalf("a live page was not recognised as live")
	}
	if Live(Registration{URL: url, Root: "/some/other/root"}, 2*time.Second) {
		t.Fatalf("a page answering for a different root counted as live")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("page did not stop")
	}
	if _, ok := Registered(dir, root); ok {
		t.Fatalf("registration survived shutdown")
	}
	if Live(reg, time.Second) {
		t.Fatalf("page still answers after shutdown")
	}
}

// A stale registration's PID may now belong to something else and must never
// be signalled: Stop proves liveness through the page, not the PID. The PID
// here is the test's own, so a wrong signal would end this process.
func TestStopNeverSignalsAStaleRegistration(t *testing.T) {
	dir := t.TempDir()
	if err := Register(dir, Registration{PID: os.Getpid(), URL: "http://127.0.0.1:1/", Root: "/repo/stale"}); err != nil {
		t.Fatal(err)
	}
	got, live, err := Stop(dir, "/repo/stale", time.Second)
	if err != nil || live || got.PID != os.Getpid() {
		t.Fatalf("stop: %+v live=%v err=%v", got, live, err)
	}
	if _, ok := Registered(dir, "/repo/stale"); ok {
		t.Fatalf("stale registration was kept")
	}
}

func TestStopPreservesNewRegistrationDuringProbe(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	replacement := Registration{PID: os.Getpid() + 1, URL: "http://127.0.0.1:1/", Root: root}
	registered := make(chan error, 1)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A new page registers while Stop is checking the old endpoint.
		registered <- Register(dir, replacement)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer probe.Close()
	old := Registration{PID: os.Getpid(), URL: probe.URL, Root: root}
	if err := Register(dir, old); err != nil {
		t.Fatal(err)
	}
	got, live, err := Stop(dir, root, time.Second)
	if err != nil || live || got.PID != old.PID {
		t.Fatalf("stop: %+v live=%v err=%v", got, live, err)
	}
	if err := <-registered; err != nil {
		t.Fatal(err)
	}
	if got, ok := Registered(dir, root); !ok || got.PID != replacement.PID || got.URL != replacement.URL {
		t.Fatalf("replacement registration was lost: %+v present=%v", got, ok)
	}
}

func TestRegistryMutationsWaitForLock(t *testing.T) {
	for _, remove := range []bool{false, true} {
		name := "register"
		if remove {
			name = "unregister"
		}
		t.Run(name, func(t *testing.T) {
			dir, root := t.TempDir(), t.TempDir()
			reg := Registration{PID: os.Getpid(), URL: "http://127.0.0.1:1/", Root: root}
			if remove {
				if err := Register(dir, reg); err != nil {
					t.Fatal(err)
				}
			}
			lock, err := fs.AcquireIndexLock(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()
			done := make(chan error, 1)
			go func() {
				if remove {
					done <- Unregister(dir, root, reg.PID)
				} else {
					done <- Register(dir, reg)
				}
			}()
			select {
			case err := <-done:
				t.Fatalf("registry mutation bypassed the held lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if _, present := Registered(dir, root); present != remove {
				t.Fatal("registration changed while another writer held the lock")
			}
			if err := lock.Release(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("registry mutation did not complete after the lock was released")
			}
		})
	}
}
