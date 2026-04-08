package dhcpd

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sylgeist/dnstonetbox/model"
)

func TestSync_WritesHostDeclarations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")

	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
		{Name: "host2.example.com", IPv4: net.ParseIP("192.168.1.20"), MAC: "11:22:33:44:55:66"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "host host1 {")
	assertContains(t, content, "hardware ethernet aa:bb:cc:dd:ee:ff;")
	assertContains(t, content, "fixed-address 192.168.1.10;")
	assertContains(t, content, `option host-name "host1";`)
	assertContains(t, content, "host host2 {")
	assertContains(t, content, "hardware ethernet 11:22:33:44:55:66;")
}

func TestSync_SkipsHostWithoutMAC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")

	hosts := []model.Host{
		{Name: "no-mac.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: ""},
		{Name: "has-mac.example.com", IPv4: net.ParseIP("192.168.1.11"), MAC: "aa:bb:cc:dd:ee:ff"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	if strings.Contains(content, "no-mac") {
		t.Errorf("host without MAC should be skipped:\n%s", content)
	}
	assertContains(t, content, "host has-mac {")
}

func TestSync_SkipsHostWithoutIPv4(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")

	hosts := []model.Host{
		{Name: "v6only.example.com", IPv6: net.ParseIP("2001:db8::1"), MAC: "aa:bb:cc:dd:ee:ff"},
		{Name: "has-v4.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "11:22:33:44:55:66"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	if strings.Contains(content, "v6only") {
		t.Errorf("IPv6-only host should be skipped:\n%s", content)
	}
	assertContains(t, content, "host has-v4 {")
}

func TestSync_UsesFirstDNSLabel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "myhost.sub.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "host myhost {")
	if strings.Contains(content, "myhost.sub.example.com") {
		t.Error("identifier should be first label only, not full FQDN")
	}
}

func TestSync_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}
	cfg := Config{ConfigFile: path}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	info1, _ := os.Stat(path)

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	info2, _ := os.Stat(path)

	if info1.ModTime() != info2.ModTime() {
		t.Error("config file was rewritten on second Sync with identical hosts")
	}
}

func TestSync_ReloadCalledOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{ConfigFile: path, ReloadCmd: "touch " + flag}
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Error("reload was not called after initial write")
	}
}

func TestSync_ReloadNotCalledWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{ConfigFile: path, ReloadCmd: "touch " + flag}
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}

	Sync(cfg, hosts, false, false) //nolint:errcheck // first write
	_ = os.Remove(flag)

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Error("reload was called even though content did not change")
	}
}

func TestSync_SkipsWhenNoConfigFile(t *testing.T) {
	if err := Sync(Config{}, nil, false, false); err != nil {
		t.Errorf("Sync with empty ConfigFile: %v", err)
	}
}

// --- Sync: dry-run ---

func TestSync_DryRun_DoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}
	cfg := Config{ConfigFile: path}

	if err := Sync(cfg, hosts, true, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("dry-run must not write any files")
	}
}

func TestSync_DryRun_DoesNotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{ConfigFile: path, ReloadCmd: "touch " + flag}
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"}}

	if err := Sync(cfg, hosts, true, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Error("dry-run must not call the reload command")
	}
}

// --- unifiedDiff ---

func TestUnifiedDiff_IdenticalContent(t *testing.T) {
	got := unifiedDiff("dhcpd.conf", []byte("line1\nline2\n"), []byte("line1\nline2\n"))
	if got != "" {
		t.Errorf("expected empty string for identical content, got:\n%s", got)
	}
}

func TestUnifiedDiff_Addition(t *testing.T) {
	old := []byte("line1\nline2\n")
	newContent := []byte("line1\nline2\nline3\n")
	got := unifiedDiff("dhcpd.conf", old, newContent)
	if !strings.Contains(got, "+line3") {
		t.Errorf("diff missing added line, got:\n%s", got)
	}
	if !strings.Contains(got, "@@") {
		t.Errorf("diff missing hunk header, got:\n%s", got)
	}
}

func TestUnifiedDiff_Removal(t *testing.T) {
	old := []byte("line1\nline2\nline3\n")
	newContent := []byte("line1\nline3\n")
	got := unifiedDiff("dhcpd.conf", old, newContent)
	if !strings.Contains(got, "-line2") {
		t.Errorf("diff missing removed line, got:\n%s", got)
	}
}

func TestUnifiedDiff_NewFile(t *testing.T) {
	got := unifiedDiff("dhcpd.conf", nil, []byte("line1\nline2\n"))
	if !strings.Contains(got, "+line1") || !strings.Contains(got, "+line2") {
		t.Errorf("diff for new file missing content, got:\n%s", got)
	}
	if !strings.Contains(got, "@@ -1,0") {
		t.Errorf("new-file diff should have @@ -1,0 header, got:\n%s", got)
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
