package ui

import (
	"context"
	"os"
	"testing"
	"time"
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
