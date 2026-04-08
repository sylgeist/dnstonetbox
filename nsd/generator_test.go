package nsd

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sylgeist/dnstonetbox/model"
)

// --- isReverseZone ---

func TestIsReverseZone(t *testing.T) {
	tests := []struct {
		zone string
		want bool
	}{
		{"example.com", false},
		{"1.168.192.in-addr.arpa", true},
		{"168.192.in-addr.arpa", true},
		{"0.0.1.0.8.b.d.0.1.0.0.2.ip6.arpa", true},
		{"in-addr.arpa.example.com", false},
	}
	for _, tt := range tests {
		if got := isReverseZone(tt.zone); got != tt.want {
			t.Errorf("isReverseZone(%q) = %v, want %v", tt.zone, got, tt.want)
		}
	}
}

// --- ipv4PTRRelName ---

func TestIPv4PTRRelName(t *testing.T) {
	tests := []struct {
		ip      string
		zone    string
		wantRel string
		wantOK  bool
	}{
		// /24 zone — single trailing octet
		{"192.168.1.10", "1.168.192.in-addr.arpa", "10", true},
		{"192.168.1.1", "1.168.192.in-addr.arpa", "1", true},
		// /16 zone — two trailing octets, reversed
		{"192.168.1.10", "168.192.in-addr.arpa", "10.1", true},
		{"192.168.2.5", "168.192.in-addr.arpa", "5.2", true},
		// IP not in zone
		{"10.0.0.1", "1.168.192.in-addr.arpa", "", false},
		{"192.168.2.1", "1.168.192.in-addr.arpa", "", false},
		// Wrong zone type
		{"192.168.1.1", "example.com", "", false},
		// IPv6 address — must fail
		{"2001:db8::1", "1.168.192.in-addr.arpa", "", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tt.ip)
		}
		rel, ok := ipv4PTRRelName(ip, tt.zone)
		if ok != tt.wantOK {
			t.Errorf("ipv4PTRRelName(%s, %s): ok=%v, want %v", tt.ip, tt.zone, ok, tt.wantOK)
			continue
		}
		if ok && rel != tt.wantRel {
			t.Errorf("ipv4PTRRelName(%s, %s): rel=%q, want %q", tt.ip, tt.zone, rel, tt.wantRel)
		}
	}
}

// --- ipv6PTRRelName ---

func TestIPv6PTRRelName(t *testing.T) {
	// Zone for 2001:db8:100::/48
	zone48 := "0.0.1.0.8.b.d.0.1.0.0.2.ip6.arpa"
	// Zone for 2001:db8::/32
	zone32 := "8.b.d.0.1.0.0.2.ip6.arpa"

	tests := []struct {
		ip      string
		zone    string
		wantRel string
		wantOK  bool
	}{
		{
			ip:      "2001:db8:100::1",
			zone:    zone48,
			wantRel: "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",
			wantOK:  true,
		},
		{
			ip:      "2001:db8:100::ff",
			zone:    zone48,
			wantRel: "f.f.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",
			wantOK:  true,
		},
		// IP in /32 parent zone
		{
			ip:      "2001:db8::1",
			zone:    zone32,
			wantRel: "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0",
			wantOK:  true,
		},
		// IP outside the zone
		{
			ip:     "2001:db9::1",
			zone:   zone48,
			wantOK: false,
		},
		// IPv4-mapped — must fail (not a true IPv6 address)
		{
			ip:     "192.168.1.1",
			zone:   zone48,
			wantOK: false,
		},
		// Wrong zone suffix
		{
			ip:     "2001:db8::1",
			zone:   "example.com",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tt.ip)
		}
		rel, ok := ipv6PTRRelName(ip, tt.zone)
		if ok != tt.wantOK {
			t.Errorf("ipv6PTRRelName(%s, %s): ok=%v, want %v", tt.ip, tt.zone, ok, tt.wantOK)
			continue
		}
		if ok && rel != tt.wantRel {
			t.Errorf("ipv6PTRRelName(%s, %s):\n  got  %q\n  want %q", tt.ip, tt.zone, rel, tt.wantRel)
		}
	}
}

// --- Sync: forward zone ---

func newZoneCfg(name string) ZoneConfig {
	return ZoneConfig{
		Name:      name,
		TTL:       ttlValue{"3600"},
		PrimaryNS: "ns1.example.com.",
		NS:        []string{"ns1.example.com."},
		Email:     "hostmaster.example.com.",
	}
}

func TestSync_ForwardZone(t *testing.T) {
	dir := t.TempDir()
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
		{Name: "host2.example.com", IPv6: net.ParseIP("2001:db8::2")},
		{Name: "dual.example.com", IPv4: net.ParseIP("192.168.1.20"), IPv6: net.ParseIP("2001:db8::20")},
		{Name: "other.org", IPv4: net.ParseIP("10.0.0.1")}, // different zone — must not appear
	}

	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg("example.com")}}
	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readZone(t, dir, "example.com")
	assertContains(t, content, "host1 IN A 192.168.1.10")
	assertContains(t, content, "host2 IN AAAA 2001:db8::2")
	assertContains(t, content, "dual IN A 192.168.1.20")
	assertContains(t, content, "dual IN AAAA 2001:db8::20")
	assertNotContains(t, content, "other.org")
}

func TestSync_ForwardZone_Idempotent(t *testing.T) {
	dir := t.TempDir()
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
	}
	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg("example.com")}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	info1, _ := os.Stat(filepath.Join(dir, "example.com.zone"))

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	info2, _ := os.Stat(filepath.Join(dir, "example.com.zone"))

	if info1.ModTime() != info2.ModTime() {
		t.Error("zone file was rewritten on second Sync with identical hosts")
	}
}

func TestSync_ForwardZone_ReloadOnChange(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{
		ZonesDir:  dir,
		ReloadCmd: "touch " + flag,
		Zones:     []ZoneConfig{newZoneCfg("example.com")},
	}
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(flag); err != nil {
		t.Error("reload command was not called after initial write")
	}
}

// --- Sync: IPv4 reverse zone ---

func TestSync_ReverseZoneIPv4(t *testing.T) {
	dir := t.TempDir()
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
		{Name: "host2.example.com", IPv4: net.ParseIP("192.168.1.20")},
		{Name: "other.example.com", IPv4: net.ParseIP("10.0.0.1")}, // different network
	}

	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg("1.168.192.in-addr.arpa")}}
	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readZone(t, dir, "1.168.192.in-addr.arpa")
	assertContains(t, content, "10 IN PTR host1.example.com.")
	assertContains(t, content, "20 IN PTR host2.example.com.")
	assertNotContains(t, content, "other.example.com")
}

func TestSync_ReverseZoneIPv4_Idempotent(t *testing.T) {
	dir := t.TempDir()
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
	}
	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg("1.168.192.in-addr.arpa")}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	info1, _ := os.Stat(filepath.Join(dir, "1.168.192.in-addr.arpa.zone"))

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	info2, _ := os.Stat(filepath.Join(dir, "1.168.192.in-addr.arpa.zone"))

	if info1.ModTime() != info2.ModTime() {
		t.Error("zone file was rewritten on second Sync with identical hosts")
	}
}

// --- Sync: IPv6 reverse zone ---

func TestSync_ReverseZoneIPv6(t *testing.T) {
	dir := t.TempDir()
	zone := "0.0.1.0.8.b.d.0.1.0.0.2.ip6.arpa" // 2001:db8:100::/48
	hosts := []model.Host{
		{Name: "host1.example.com", IPv6: net.ParseIP("2001:db8:100::1")},
		{Name: "host2.example.com", IPv6: net.ParseIP("2001:db8:100::ff")},
		{Name: "other.example.com", IPv6: net.ParseIP("2001:db9::1")}, // different prefix
	}

	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg(zone)}}
	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readZone(t, dir, zone)
	assertContains(t, content, "IN PTR host1.example.com.")
	assertContains(t, content, "IN PTR host2.example.com.")
	assertNotContains(t, content, "other.example.com")
}

// --- Sync: dry-run ---

func TestSync_DryRun_DoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	hosts := []model.Host{
		{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")},
	}
	cfg := Config{ZonesDir: dir, Zones: []ZoneConfig{newZoneCfg("example.com")}}

	if err := Sync(cfg, hosts, true, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	path := filepath.Join(dir, "example.com.zone")
	if _, err := os.Stat(path); err == nil {
		t.Error("dry-run must not write any files")
	}
}

func TestSync_DryRun_DoesNotReload(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(t.TempDir(), "reloaded")

	cfg := Config{
		ZonesDir:  dir,
		ReloadCmd: "touch " + flag,
		Zones:     []ZoneConfig{newZoneCfg("example.com")},
	}
	hosts := []model.Host{{Name: "host1.example.com", IPv4: net.ParseIP("192.168.1.10")}}

	if err := Sync(cfg, hosts, true, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(flag); err == nil {
		t.Error("dry-run must not call the reload command")
	}
}

// --- unifiedDiff ---

func TestUnifiedDiff_IdenticalContent(t *testing.T) {
	got := unifiedDiff("test.zone", []byte("line1\nline2\n"), []byte("line1\nline2\n"))
	if got != "" {
		t.Errorf("expected empty string for identical content, got:\n%s", got)
	}
}

func TestUnifiedDiff_Addition(t *testing.T) {
	old := []byte("line1\nline2\n")
	newContent := []byte("line1\nline2\nline3\n")
	got := unifiedDiff("test.zone", old, newContent)
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
	got := unifiedDiff("test.zone", old, newContent)
	if !strings.Contains(got, "-line2") {
		t.Errorf("diff missing removed line, got:\n%s", got)
	}
}

func TestUnifiedDiff_NewFile(t *testing.T) {
	got := unifiedDiff("new.zone", nil, []byte("line1\nline2\n"))
	if !strings.Contains(got, "+line1") || !strings.Contains(got, "+line2") {
		t.Errorf("diff for new file missing content, got:\n%s", got)
	}
	if !strings.Contains(got, "@@ -1,0") {
		t.Errorf("new-file diff should have @@ -1,0 header, got:\n%s", got)
	}
}

// --- helpers ---

func readZone(t *testing.T, dir, zone string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, zone+".zone"))
	if err != nil {
		t.Fatalf("read zone file %s: %v", zone, err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("zone content missing %q\n--- content ---\n%s", substr, content)
	}
}

func assertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("zone content unexpectedly contains %q\n--- content ---\n%s", substr, content)
	}
}
