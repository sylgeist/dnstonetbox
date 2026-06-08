# Diskless DHCP Netboot Options Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit per-host netboot options (`filename`, `option root-path`, optional `next-server`) in the OpenBSD dhcpd output for hosts whose NetBox IP carries a `diskless_arch` custom field.

**Architecture:** Extend the NetBox fetch to read the `diskless_arch` custom field into a new `model.Host.DisklessArch` field; add an optional `netboot` block to the dhcpd `Config`; the dhcpd generator appends the netboot lines inside a host's stanza only when netboot is configured and the host has an arch. Non-diskless output is byte-for-byte unchanged.

**Tech Stack:** Go 1.24, stdlib only (`net/http`, `encoding/json`), `gopkg.in/yaml.v3` for config, `go test -race`.

**Spec:** `docs/superpowers/specs/2026-06-08-diskless-dhcp-netboot-design.md`

---

## File structure

| File | Change |
|---|---|
| `model/host.go` | add `DisklessArch string` field |
| `netbox/client.go` | add `custom_fields.diskless_arch` to `ipEntry`; copy into `Host` during merge |
| `netbox/client_test.go` | extend `ipFixture` with custom fields; add a diskless-arch test |
| `dhcpd/generator.go` | add `Netboot` struct + `Config.Netboot`; defaulting + emission logic |
| `dhcpd/generator_test.go` | add netboot emission tests |
| `config.example.yaml` | commented `netboot:` sub-block under `dhcpd` |
| `README.md` | dhcpd section: netboot fields + NetBox custom-field setup |
| `CLAUDE.md` | note the netboot extension |

Conventions to follow: tests use `t.TempDir()` + the existing `assertContains`/`readFile` helpers (dhcpd) and the `singlePageServer`/`ipFixture` mock (netbox). All code must be `gofmt`-clean and pass `make vet`.

---

### Task 1: Read `diskless_arch` from NetBox into the model

**Files:**
- Modify: `model/host.go`
- Modify: `netbox/client.go`
- Test: `netbox/client_test.go`

- [ ] **Step 1: Add the failing test** in `netbox/client_test.go`

First extend the fixture types (top of the file, near the existing `ipFixture`/`macFixture`) to allow injecting custom fields:

```go
type ipFixture struct {
	Address        string      `json:"address"`
	DNSName        string      `json:"dns_name"`
	AssignedObject *macFixture `json:"assigned_object"`
	CustomFields   *cfFixture  `json:"custom_fields,omitempty"`
}

type cfFixture struct {
	DisklessArch string `json:"diskless_arch"`
}
```

Then add the test:

```go
func TestFetchHosts_DisklessArchCustomField(t *testing.T) {
	srv := singlePageServer(t, []ipFixture{
		{Address: "192.168.10.20/24", DNSName: "office-nuc.home.arpa",
			AssignedObject: &macFixture{MACAddress: "aa:bb:cc:dd:ee:ff"},
			CustomFields:   &cfFixture{DisklessArch: "amd64"}},
		{Address: "192.168.10.30/24", DNSName: "plain.home.arpa",
			AssignedObject: &macFixture{MACAddress: "11:22:33:44:55:66"}},
	})
	defer srv.Close()

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	got := map[string]string{}
	for _, h := range hosts {
		got[h.Name] = h.DisklessArch
	}
	if got["office-nuc.home.arpa"] != "amd64" {
		t.Errorf("DisklessArch = %q, want amd64", got["office-nuc.home.arpa"])
	}
	if got["plain.home.arpa"] != "" {
		t.Errorf("DisklessArch = %q, want empty for non-diskless host", got["plain.home.arpa"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./netbox/ -run TestFetchHosts_DisklessArchCustomField -v`
Expected: FAIL — compile error, `h.DisklessArch undefined (type model.Host has no field or method DisklessArch)`.

- [ ] **Step 3: Add the model field** in `model/host.go`

```go
// Host represents a network host as synthesized from NetBox data.
// It is the shared data model passed to each generator.
type Host struct {
	Name         string // fully qualified domain name, no trailing dot (e.g. "host1.example.com")
	IPv4         net.IP
	IPv6         net.IP
	MAC          string // hardware ethernet address for DHCP (e.g. "00:11:22:33:44:55"), may be empty
	DisklessArch string // loader arch for diskless netboot (e.g. "amd64"/"arm64"); "" = not diskless
}
```

- [ ] **Step 4: Read the custom field in the client** — `netbox/client.go`

Extend `ipEntry` (the response struct near the bottom of the file):

```go
// ipEntry maps the fields we care about from /api/ipam/ip-addresses/.
type ipEntry struct {
	Address        string          `json:"address"`
	DNSName        string          `json:"dns_name"`
	AssignedObject *assignedObject `json:"assigned_object"`
	CustomFields   struct {
		DisklessArch string `json:"diskless_arch"` // "" when unset/null
	} `json:"custom_fields"`
}
```

In `FetchHosts`, inside the `for _, entry := range page.Results` loop, right after the existing MAC-copy block, add:

```go
		if h.DisklessArch == "" && entry.CustomFields.DisklessArch != "" {
			h.DisklessArch = entry.CustomFields.DisklessArch
		}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./netbox/ -run TestFetchHosts_DisklessArchCustomField -v`
Expected: PASS.

- [ ] **Step 6: Run the full netbox package to confirm no regressions**

Run: `go test ./netbox/ -count=1`
Expected: `ok  github.com/sylgeist/dnstonetbox/netbox`

- [ ] **Step 7: Commit**

```bash
git add model/host.go netbox/client.go netbox/client_test.go
git commit -m "feat(netbox): read diskless_arch custom field into Host"
```

---

### Task 2: Emit netboot options in the dhcpd generator

**Files:**
- Modify: `dhcpd/generator.go`
- Test: `dhcpd/generator_test.go`

- [ ] **Step 1: Add the failing tests** in `dhcpd/generator_test.go`

```go
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
	assertContains(t, content, `filename "arm64/loader.efi";`) // {arch} substitution
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./dhcpd/ -run 'TestSync_(Diskless|NextServer|NonDiskless|RootBase|Idempotent)' -v`
Expected: FAIL — compile error, `undefined: Netboot` and `unknown field Netboot in struct literal`.

- [ ] **Step 3: Add the `Netboot` type and `Config.Netboot` field** in `dhcpd/generator.go`

Replace the existing `Config` struct (lines 15-19) with:

```go
// Netboot holds optional settings for emitting FreeBSD diskless netboot options
// (filename / root-path / next-server) for hosts that carry a DisklessArch.
type Netboot struct {
	NFSServer      string `yaml:"nfs_server"`      // required to enable netboot
	RootBase       string `yaml:"root_base"`       // default "/diskless/hosts"
	NextServer     string `yaml:"next_server"`     // optional; line omitted if empty
	LoaderFilename string `yaml:"loader_filename"` // default "{arch}/loader.efi"
}

// Config holds DHCPD generator settings.
type Config struct {
	ConfigFile string   `yaml:"config_file"` // path to generated include file
	ReloadCmd  string   `yaml:"reload_cmd"`  // e.g. "rcctl restart dhcpd"
	Netboot    *Netboot `yaml:"netboot"`     // optional; nil disables netboot options
}
```

- [ ] **Step 4: Add the emission logic** in `dhcpd/generator.go`

In `Sync`, immediately after the `var buf bytes.Buffer` / header `Fprintln` and before the `count := 0` loop, compute the effective netboot defaults:

```go
	nb := cfg.Netboot
	netbootActive := nb != nil && nb.NFSServer != ""
	rootBase := "/diskless/hosts"
	loaderTmpl := "{arch}/loader.efi"
	if nb != nil {
		if nb.RootBase != "" {
			rootBase = strings.TrimRight(nb.RootBase, "/")
		}
		if nb.LoaderFilename != "" {
			loaderTmpl = nb.LoaderFilename
		}
	}
```

Then inside the host loop, after the existing `option host-name` line (`fmt.Fprintf(&buf, "\toption host-name ...")`) and before the closing `fmt.Fprintln(&buf, "}")`, add:

```go
		if h.DisklessArch != "" {
			if netbootActive {
				loader := strings.ReplaceAll(loaderTmpl, "{arch}", h.DisklessArch)
				fmt.Fprintf(&buf, "\tfilename \"%s\";\n", loader)
				fmt.Fprintf(&buf, "\toption root-path \"%s:%s/%s\";\n", nb.NFSServer, rootBase, label)
				if nb.NextServer != "" {
					fmt.Fprintf(&buf, "\tnext-server %s;\n", nb.NextServer)
				}
			} else {
				log.Printf("dhcpd: host %s has diskless_arch=%q but dhcpd.netboot is not configured; emitting plain host", h.Name, h.DisklessArch)
			}
		}
```

(`label` is the existing short-label variable; `log` and `strings` are already imported.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./dhcpd/ -run 'TestSync_(Diskless|NextServer|NonDiskless|RootBase|Idempotent)' -v`
Expected: PASS (all six new tests).

- [ ] **Step 6: Run the full dhcpd package to confirm no regressions**

Run: `go test ./dhcpd/ -count=1`
Expected: `ok  github.com/sylgeist/dnstonetbox/dhcpd` (existing tests still pass — non-diskless output unchanged).

- [ ] **Step 7: Commit**

```bash
git add dhcpd/generator.go dhcpd/generator_test.go
git commit -m "feat(dhcpd): emit netboot options for diskless hosts"
```

---

### Task 3: Documentation

**Files:**
- Modify: `config.example.yaml`
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update `config.example.yaml`** — replace the `dhcpd:` block at the end with:

```yaml
dhcpd:
  # Path to the generated include file. Add to dhcpd.conf:
  #   include "/etc/dhcpd.d/static-hosts.conf";
  config_file: /etc/dhcpd.d/static-hosts.conf
  reload_cmd: rcctl restart dhcpd

  # Optional: emit FreeBSD diskless netboot options for hosts whose NetBox IP
  # has a `diskless_arch` custom field (selection: amd64/arm64/...). Omit the
  # whole netboot block to disable. nfs_server is required to enable it.
  # netboot:
  #   nfs_server: 192.168.10.10          # FreeBSD ZFS/NFS server IP
  #   root_base: /diskless/hosts          # default; root-path = nfs_server:root_base/<short-host>
  #   next_server: 192.168.10.1           # optional; omit if TFTP runs on the dhcpd host
  #   loader_filename: "{arch}/loader.efi" # default; {arch} replaced by diskless_arch
```

- [ ] **Step 2: Update `README.md`** — in the `### dhcpd` section, after the existing fields table (after the line `| `reload_cmd` | no | ... |`), append:

```markdown

**Netboot (diskless) options:** set the optional `netboot` sub-block to emit
`filename`, `option root-path`, and (optionally) `next-server` for hosts whose
NetBox IP address carries a `diskless_arch` custom field. Create that custom
field in NetBox on the IP address object (Selection type, e.g. `amd64`, `arm64`);
its value selects the loader architecture and its presence flags the host as a
netboot client. Non-diskless hosts are unaffected.

| Field | Required | Description |
|---|---|---|
| `nfs_server` | yes (to enable) | NFS server IP used in `root-path`; enables netboot when set |
| `root_base` | no | Base path for `root-path`. Default `/diskless/hosts`. Result: `<nfs_server>:<root_base>/<short-host>` |
| `next_server` | no | TFTP server. Omitted from output when empty (dhcpd then defaults it to itself) |
| `loader_filename` | no | Template with `{arch}` placeholder. Default `{arch}/loader.efi` |

Example diskless host output:
```

host office-nuc.home.arpa {
    hardware ethernet aa:bb:cc:dd:ee:ff;
    fixed-address 192.168.10.20;
    option host-name "office-nuc";
    filename "amd64/loader.efi";
    option root-path "192.168.10.10:/diskless/hosts/office-nuc";
    next-server 192.168.10.1;
}
```
```

(Note: the inner fenced block above is the example output; keep the surrounding triple-backticks correct when inserting.)

- [ ] **Step 3: Update `CLAUDE.md`** — under "Key design decisions", after the `**DHCPD output**` bullet, add:

```markdown
- **DHCPD netboot options** — when `dhcpd.netboot` is configured (requires `nfs_server`) and a host's `model.Host.DisklessArch` is non-empty (from the NetBox `diskless_arch` custom field), the host stanza also gets `filename`, `option root-path` (`<nfs_server>:<root_base>/<short-label>`), and an optional `next-server`. Hosts without an arch, or any output when the netboot block is absent, are byte-for-byte unchanged. A host with an arch but no netboot config logs a warning and emits a plain stanza.
```

- [ ] **Step 4: Verify docs don't break the build and tests still pass**

Run: `go build ./... && go test ./... -count=1`
Expected: build succeeds; all packages `ok`.

- [ ] **Step 5: Commit**

```bash
git add config.example.yaml README.md CLAUDE.md
git commit -m "docs: document dhcpd netboot options"
```

---

### Task 4: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Format check**

Run: `gofmt -l model/ netbox/ dhcpd/`
Expected: no output (all files already formatted). If any file is listed, run `gofmt -w <file>` and amend the relevant commit.

- [ ] **Step 2: Vet + full race test suite**

Run: `make vet && make test`
Expected: `go vet ./...` clean; `go test -race -count=1 ./...` reports `ok` for all packages.

- [ ] **Step 3: Build the binary**

Run: `make build`
Expected: `./dnstonetbox` builds with no errors.

- [ ] **Step 4: Sanity-check generated output end-to-end (optional, no NetBox needed)**

This is covered by the unit tests; no live NetBox run is required. If a live check is desired later, set the `diskless_arch` custom field on a test IP in NetBox and run `./dnstonetbox --config config.yaml --once --dry-run --verbose` to preview the dhcpd diff.

---

## Plan self-review

- **Spec coverage:** §1 NetBox schema/fetch/model → Task 1; §2 config struct → Task 2 Step 3; §2 emission rule + `{arch}` + next-server optionality + root_base default/trim → Task 2 Steps 4–5; edge case "arch set, netboot nil" → Task 2 `TestSync_DisklessArchWithoutNetbootConfigEmitsPlainHost` + warning log; non-diskless unchanged + idempotency → Task 2 tests; testing section (netbox + dhcpd + docs) → Tasks 1–3; docs (README/config.example/CLAUDE) → Task 3. Open item #1 (custom field decodes as plain string) is exercised by Task 1's test fixture.
- **Placeholder scan:** none — every step has concrete code/commands and expected output.
- **Type consistency:** `model.Host.DisklessArch` (Task 1) is the field read in `dhcpd` (Task 2); `Netboot` struct field names (`NFSServer`/`RootBase`/`NextServer`/`LoaderFilename`) and YAML keys (`nfs_server`/`root_base`/`next_server`/`loader_filename`) match between the struct definition, the tests, and `config.example.yaml`/README.
