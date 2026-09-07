package ui

import "testing"

func TestParseServerCommandLineRecognisesOnlyAociMCPServers(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		server bool
		root   string
	}{
		{"repo flag", []string{"/opt/aoci", "--repo", "/srv/one", "mcp"}, true, "/srv/one"},
		{"repo equals", []string{"/opt/aoci", "--repo=/srv/two", "mcp"}, true, "/srv/two"},
		{"windows name", []string{`C:\tools\aoci.exe`, "mcp", "--repo", "/srv/three"}, true, "/srv/three"},
		{"no repo flag", []string{"aoci", "mcp"}, true, ""},
		{"not a server", []string{"/opt/aoci", "--repo", "/srv/one", "verify"}, false, ""},
		{"other binary", []string{"/usr/bin/python3", "mcp"}, false, ""},
		{"too short", []string{"aoci"}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instance, ok := parseServerCommandLine(4242, tc.args)
			if ok != tc.server {
				t.Fatalf("server=%v, want %v", ok, tc.server)
			}
			if ok && instance.Root != tc.root {
				t.Fatalf("root=%q, want %q", instance.Root, tc.root)
			}
			if ok && instance.PID != 4242 {
				t.Fatalf("pid=%d", instance.PID)
			}
		})
	}
}
