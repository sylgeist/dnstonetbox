package unbound

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sylgeist/dnstonetbox/model"
)

func TestSync_WritesLocalData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-hosts.conf")

	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
		{Name: "host2.example.com", IPv6: net.ParseIP("2001:db8::2")},
		{Name: "dual.example.com", IPv4: net.ParseIP("192.168.1.20"), IPv6: net.ParseIP("2001:db8::20")},
	}

	cfg := Config{ConfigFile: path, TTL: 3600}
	if err := Sync(cfg, hosts); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	// Forward records
	assertContains(t, content, `local-data: "host1.example.com. 3600 IN A 192.168.1.10"`)
	assertContains(t, content, `local-data: "host2.example.com. 3600 IN AAAA 2001:db8::2"`)
	assertContains(t, content, `local-data: "dual.example.com. 3600 IN A 192.168.1.20"`)
	assertContains(t, content, `local-data: "dual.example.com. 3600 IN AAAA 2001:db8::20"`)
	// Reverse records
	assertContains(t, content, `local-data-ptr: "192.168.1.10 3600 host1.example.com."`)
	assertContains(t, content, `local-data-ptr: "2001:db8::2 3600 host2.example.com."`)
}

func TestSync_DefaultTTL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-hosts.conf")
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}

	if err := (Sync(Config{ConfigFile: path}, hosts)); err != nil { // TTL omitted → default 3600
		t.Fatalf("Sync: %v", err)
	}
	assertContains(t, readFile(t, path), "3600 IN A")
}

func TestSync_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-hosts.conf")
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}
	cfg := Config{ConfigFile: path}

	if err := Sync(cfg, hosts); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	info1, _ := os.Stat(path)

	if err := Sync(cfg, hosts); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	info2, _ := os.Stat(path)

	if info1.ModTime() != info2.ModTime() {
		t.Error("config file was rewritten on second Sync with identical hosts")
	}
}

func TestSync_ReloadCalledOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-hosts.conf")
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{ConfigFile: path, ReloadCmd: "touch " + flag}
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}

	if err := Sync(cfg, hosts); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Error("reload was not called after initial write")
	}
}

func TestSync_ReloadNotCalledWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "local-hosts.conf")
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{ConfigFile: path, ReloadCmd: "touch " + flag}
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}

	Sync(cfg, hosts) //nolint:errcheck // first write
	os.Remove(flag)

	if err := Sync(cfg, hosts); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Error("reload was called even though content did not change")
	}
}

func TestSync_SkipsWhenNoConfigFile(t *testing.T) {
	// Should not error or create any file when ConfigFile is empty.
	if err := Sync(Config{}, nil); err != nil {
		t.Errorf("Sync with empty ConfigFile: %v", err)
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("content missing %q\n--- content ---\n%s", substr, content)
	}
}
