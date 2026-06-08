# Diskless DHCP Netboot Options — Design

**Date:** 2026-06-08
**Status:** Approved design, pre-implementation
**Repo:** dnstonetbox

## Goal

Let `dnstonetbox` emit the per-host netboot options FreeBSD diskless clients need
(`filename`, `option root-path`, optional `next-server`) in the OpenBSD dhcpd
static-host output, driven by a NetBox custom field. This makes NetBox the single
source of truth for a diskless fleet's DNS *and* netboot DHCP, replacing
hand-maintained `hosts.tab` + `gen-dhcpd.sh` in the companion `diskless` project.

## Background

`dnstonetbox` fetches IP/DNS data from NetBox and renders config for NSD, Unbound,
and OpenBSD dhcpd, reloading each service only when output changes. The dhcpd
generator (`dhcpd/generator.go`) currently emits, per host with a MAC + IPv4:

```
host <fqdn> {
    hardware ethernet <mac>;
    fixed-address <ipv4>;
    option host-name "<short-label>";
}
```

The companion FreeBSD diskless setup additionally requires, per diskless host:
`filename "<arch>/loader.efi"`, `option root-path "<nfs>:/diskless/hosts/<name>"`,
and optionally `next-server <tftp>`. This feature adds exactly those, gated on a
NetBox custom field, with no change to non-diskless output.

## Settled decisions

- **Marking a host diskless:** a NetBox custom field **`diskless_arch`** on the
  **IP address** object (Selection type: `amd64`, `arm64`, … extensible). Non-empty
  value = diskless; the value is the loader architecture.
- **root-path source:** one **global** NFS server + base path (no per-host
  override — YAGNI). `root-path = "<nfs_server>:<root_base>/<short-label>"`.
- **short label = ZFS dataset/host name:** the first DNS label of `dns_name`
  equals the diskless host's dataset name (relied upon, not separately stored).
- **next-server:** optional. On the target router, TFTP and DHCP run on the same
  host, so dhcpd defaults next-server to itself; the line is emitted only when
  configured.
- **Backward compatibility:** netboot config is an optional nested block; when
  absent, dhcpd output is byte-for-byte unchanged.

## Section 1 — NetBox schema, fetch, model

**NetBox setup (operator, one-time):** create custom field `diskless_arch` on the
IP address object, type *Selection*, choices `amd64`/`arm64` (add `riscv` etc.
later). Empty for non-diskless hosts.

**Fetch (`netbox/client.go`):** the `/api/ipam/ip-addresses/` response already
includes `custom_fields`, so no extra API call. Extend `ipEntry`:
```go
type ipEntry struct {
    Address        string          `json:"address"`
    DNSName        string          `json:"dns_name"`
    AssignedObject *assignedObject `json:"assigned_object"`
    CustomFields   struct {
        DisklessArch string `json:"diskless_arch"` // "" when unset/null
    } `json:"custom_fields"`
}
```
In the existing IPv4/IPv6 merge-by-hostname loop, copy a non-empty `DisklessArch`
onto the merged `Host` (mirrors how MAC is copied when first seen).

**Model (`model/host.go`):** add one field:
```go
DisklessArch string // e.g. "amd64"/"arm64"; "" = not a diskless client
```

## Section 2 — Config, emission, edge cases

**Config (`dhcpd` package):** optional nested block; pointer `nil` = feature off.
```go
type Netboot struct {
    NFSServer      string `yaml:"nfs_server"`      // required to enable netboot
    RootBase       string `yaml:"root_base"`       // default "/diskless/hosts"
    NextServer     string `yaml:"next_server"`     // optional; line omitted if empty
    LoaderFilename string `yaml:"loader_filename"` // default "{arch}/loader.efi"
}
type Config struct {
    ConfigFile string   `yaml:"config_file"`
    ReloadCmd  string   `yaml:"reload_cmd"`
    Netboot    *Netboot `yaml:"netboot"`
}
```
Netboot is **active** when `Netboot != nil && Netboot.NFSServer != ""`. In `Sync`,
apply defaults when blank: `RootBase` → `/diskless/hosts`, `LoaderFilename` →
`{arch}/loader.efi`. Trim a trailing `/` from `RootBase` when composing root-path.

**Emission rule:** inside each host block, after the `option host-name` line, *if*
netboot is active *and* `h.DisklessArch != ""`, append:
```
    filename "<LoaderFilename with {arch}→DisklessArch>";
    option root-path "<NFSServer>:<RootBase>/<short-label>";
    next-server <NextServer>;        # only when NextServer != ""
```
- short label = existing `strings.SplitN(h.Name, ".", 2)[0]`.
- `{arch}` literal in `LoaderFilename` is replaced with `h.DisklessArch`.

Example output for a diskless host:
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

**Edge cases:**
- `diskless_arch` set but netboot **not** configured (`Netboot == nil` or no
  `NFSServer`): emit a plain host (unchanged) and `log.Printf` a one-line warning
  naming the host, so it is not silently ignored.
- Diskless host missing MAC or IPv4: already skipped by the existing guard
  (cannot netboot regardless).
- Non-diskless hosts: output byte-for-byte unchanged; idempotency preserved.

## Testing

Follow existing `t.TempDir` + `assertContains` patterns.

- `dhcpd/generator_test.go`:
  - diskless host emits `filename` and `option root-path`
  - `next-server` present only when `NextServer` configured
  - `{arch}` substitution (amd64 vs arm64)
  - non-diskless host emits no netboot lines
  - `diskless_arch` set but `Netboot == nil` → plain host, no netboot lines (and
    warning path exercised)
  - idempotency holds with a diskless host present
  - `root_base` trailing-slash normalization
- `netbox/client_test.go`:
  - fixture IP with `custom_fields.diskless_arch` populates `Host.DisklessArch`
  - absent/null custom field → `DisklessArch == ""`
- Docs:
  - `README.md`: dhcpd section gains the `netboot` block + NetBox custom-field
    setup note
  - `config.example.yaml`: commented `netboot:` sub-block under `dhcpd`
  - `CLAUDE.md`: note the netboot extension under DHCPD design decisions

## Out of scope (YAGNI)

- Per-host NFS server / root-path overrides.
- Fetching arch from the Device/VM object (keeps the client single-endpoint).
- Any DHCP options beyond filename/root-path/next-server.
- Driving the FreeBSD-side clone/export from NetBox (separate future effort).

## Open items to resolve at implementation

1. Confirm NetBox returns the Selection custom field value as a plain string under
   `custom_fields.diskless_arch` (expected); adjust decode if the instance returns
   an object.
2. Decide warning verbosity for the "arch set but netboot unconfigured" case
   (once-per-sync vs per-host) — default per-host is fine given low host counts.
