# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
go mod tidy          # fetch deps, update go.sum (required before first build)
make build           # build ./dnstonetbox binary
make test            # go test -race -count=1 ./...
make vet             # go vet ./...
make lint            # vet + staticcheck (requires: go install honnef.co/go/tools/cmd/staticcheck@latest)
make cross           # build openbsd/amd64 and openbsd/arm64 binaries
make dist            # build all release binaries into dist/

go test ./nsd/       # run a single package's tests
./dnstonetbox --config config.yaml --once   # single run for testing
```

## Architecture

`dnstonetbox` fetches IP/DNS data from NetBox and renders config files for NSD, Unbound, and OpenBSD DHCPD, reloading each service only when the output has changed.

```
main.go          orchestration — flag parsing, sync loop (ticker or --once)
config.go        top-level Config struct + YAML loading; custom duration type
model/host.go    shared Host type (Name, IPv4, IPv6, MAC)
netbox/          REST API client — paginates /api/ipam/ip-addresses/, merges
                 IPv4+IPv6 entries by dns_name into a single Host per hostname
nsd/             writes $zones_dir/<zone>.zone files from a text/template;
                 calls `nsd-control reload <zone>` per changed zone
unbound/         writes a flat local-data include file; calls reload_cmd
dhcpd/           writes host { } stanzas; calls reload_cmd
```

### Data flow

1. `netbox.Client.FetchHosts()` → `[]model.Host` (source of truth)
2. Each generator (`nsd.Sync`, `unbound.Sync`, `dhcpd.Sync`) independently renders its output and calls `writeIfChanged` — a file is only written (and its service reloaded) when the content differs from what's on disk.

### Key design decisions

- **Single binary, no runtime deps** — only external dependency is `gopkg.in/yaml.v3` for config; everything else is stdlib. Deploy with `scp`.
- **Idempotent generators** — each generator is self-contained and safe to call repeatedly. `writeIfChanged` prevents unnecessary service reloads.
- **NSD zone serial** — uses a 32-bit FNV-1a hash of the zone name + record content (`contentSerial` in `nsd/generator.go`). The same host data always produces the same serial, which makes zone files idempotent and prevents spurious NSD reloads. Note: serials are not monotonically increasing if records are removed and re-added; this is acceptable for primary-only setups but may cause zone-transfer issues with secondaries.
- **NSD reverse zones** — zones ending in `.in-addr.arpa` or `.ip6.arpa` are detected automatically and rendered with PTR records instead of A/AAAA records. IPv4 PTR names are computed by stripping the network prefix (derived from the zone name) and reversing the remaining octets. IPv6 PTR names expand all 32 nibbles in reversed order and strip the zone's nibble suffix.
- **DHCPD output** — only hosts with both a MAC address and an IPv4 address are emitted. The MAC comes from NetBox's `assigned_object.mac_address` (set on the interface linked to the IP).
- **Unbound output** — generates both forward (`local-data`) and reverse (`local-data-ptr`) entries. The generated file must be referenced with `include:` in `unbound.conf`.
- **Daemon vs cron** — running with `--once` is suitable for OpenBSD cron (`/etc/cron` or `crontab`). Without `--once`, the binary polls on the configured interval.

## Config

Copy `config.example.yaml` to `config.yaml` (gitignored). The `interval` field uses Go duration syntax (`5m`, `1h`). Any generator section can be omitted entirely to skip that target.

## OpenBSD deployment notes

- Install Go: `pkg_add go`
- The binary must run as a user with write access to the zone/config directories and permission to call `nsd-control`, `unbound-control`, and `rcctl`.
- Typical setup: run as root via cron with `--once`, or run as a daemon under `rcctl` using a custom rc.d script.
