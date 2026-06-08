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
	assertContains(t, content, "host host1.example.com {")
	assertContains(t, content, "hardware ethernet aa:bb:cc:dd:ee:ff;")
	assertContains(t, content, "fixed-address 192.168.1.10;")
	assertContains(t, content, `option host-name "host1";`)
	assertContains(t, content, "host host2.example.com {")
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
	assertContains(t, content, "host has-mac.example.com {")
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
	assertContains(t, content, "host has-v4.example.com {")
}

func TestSync_StanzaUsesFQDNHostNameUsesFirstLabel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "myhost.sub.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	// The declaration name is the FQDN (guaranteed unique); the host-name
	// option carries the short first label handed to the client.
	assertContains(t, content, "host myhost.sub.example.com {")
	assertContains(t, content, `option host-name "myhost";`)
}

func TestSync_DistinctStanzaNamesForSharedLabel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	// Both hosts share the leftmost label "www"; dhcpd requires unique host
	// declaration names, so the stanza name must be the full FQDN.
	hosts := []model.Host{
		{Name: "www.x.example.com", IPv4: net.ParseIP("192.168.1.10"), MAC: "aa:bb:cc:dd:ee:ff"},
		{Name: "www.y.example.com", IPv4: net.ParseIP("192.168.2.10"), MAC: "11:22:33:44:55:66"},
	}

	if err := Sync(Config{ConfigFile: path}, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	content := readFile(t, path)
	assertContains(t, content, "host www.x.example.com {")
	assertContains(t, content, "host www.y.example.com {")
	// host-name option still carries the short label the client should use.
	assertContains(t, content, `option host-name "www";`)
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

func TestSync_DisklessHostEmitsNetbootOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "office-nuc.home.arpa", IPv4: net.ParseIP("192.168.10.20"),
			MAC: "aa:bb:cc:dd:ee:ff", DisklessArch: "amd64"},
	}
	cfg := Config{ConfigFile: path, Netboot: &Netboot{
		NFSServer: "192.168.10.10", NextServer: "192.168.10.1"}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content := readFile(t, path)
	assertContains(t, content, `filename "amd64/loader.efi";`)
	assertContains(t, content, `option root-path "192.168.10.10:/diskless/hosts/office-nuc";`)
	assertContains(t, content, "next-server 192.168.10.1;")
}

func TestSync_NextServerOmittedWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "pi.home.arpa", IPv4: net.ParseIP("192.168.10.21"),
			MAC: "dc:a6:32:11:22:33", DisklessArch: "arm64"},
	}
	cfg := Config{ConfigFile: path, Netboot: &Netboot{NFSServer: "192.168.10.10"}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content := readFile(t, path)
	assertContains(t, content, `filename "arm64/loader.efi";`)
	if strings.Contains(content, "next-server") {
		t.Errorf("next-server must be omitted when NextServer unset:\n%s", content)
	}
}

func TestSync_NonDisklessHostHasNoNetbootOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "plain.home.arpa", IPv4: net.ParseIP("192.168.10.40"), MAC: "11:22:33:44:55:66"},
	}
	cfg := Config{ConfigFile: path, Netboot: &Netboot{NFSServer: "192.168.10.10"}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content := readFile(t, path)
	assertContains(t, content, "host plain.home.arpa {")
	for _, s := range []string{"filename", "root-path", "next-server"} {
		if strings.Contains(content, s) {
			t.Errorf("non-diskless host must not contain %q:\n%s", s, content)
		}
	}
}

func TestSync_DisklessArchWithoutNetbootConfigEmitsPlainHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "office-nuc.home.arpa", IPv4: net.ParseIP("192.168.10.20"),
			MAC: "aa:bb:cc:dd:ee:ff", DisklessArch: "amd64"},
	}
	cfg := Config{ConfigFile: path} // no Netboot block

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content := readFile(t, path)
	assertContains(t, content, "host office-nuc.home.arpa {")
	if strings.Contains(content, "root-path") {
		t.Errorf("must not emit netboot options when netboot unconfigured:\n%s", content)
	}
}

func TestSync_RootBaseTrailingSlashNormalized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "office-nuc.home.arpa", IPv4: net.ParseIP("192.168.10.20"),
			MAC: "aa:bb:cc:dd:ee:ff", DisklessArch: "amd64"},
	}
	cfg := Config{ConfigFile: path, Netboot: &Netboot{
		NFSServer: "192.168.10.10", RootBase: "/diskless/hosts/"}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content := readFile(t, path)
	assertContains(t, content, `option root-path "192.168.10.10:/diskless/hosts/office-nuc";`)
}

func TestSync_IdempotentWithDisklessHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "static-hosts.conf")
	hosts := []model.Host{
		{Name: "office-nuc.home.arpa", IPv4: net.ParseIP("192.168.10.20"),
			MAC: "aa:bb:cc:dd:ee:ff", DisklessArch: "amd64"},
	}
	cfg := Config{ConfigFile: path, Netboot: &Netboot{
		NFSServer: "192.168.10.10", NextServer: "192.168.10.1"}}

	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	info1, _ := os.Stat(path)
	if err := Sync(cfg, hosts, false, false); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	info2, _ := os.Stat(path)
	if info1.ModTime() != info2.ModTime() {
		t.Error("config file rewritten on second Sync with identical diskless host")
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
