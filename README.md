# dnstonetbox

Syncs IP and DNS data from [NetBox](https://netboxlabs.com/) to config files for NSD (authoritative DNS), Unbound (resolver), and OpenBSD DHCPD. Only rewrites files and reloads services when content actually changes.

## How it works

1. Fetches all IP addresses with a `dns_name` set from NetBox's `/api/ipam/ip-addresses/` endpoint
2. Merges IPv4 and IPv6 entries by hostname into a single host record
3. Each generator (NSD, Unbound, DHCPD) independently renders its output and compares it to the file on disk — a service is only reloaded when the content differs

## Installation

Pre-built binaries for OpenBSD (amd64, arm64) and Linux (amd64) are attached to each [release](../../releases).

To build from source (requires Go 1.24+):

```sh
go mod tidy
make build          # builds ./dnstonetbox
make cross          # builds openbsd/amd64 and openbsd/arm64 binaries
make dist           # builds all release binaries into dist/
```

## Usage

```sh
dnstonetbox [flags]

  -config string    path to config file (default "config.yaml")
  -once             run once and exit, useful for cron
  -dry-run          show what would change without writing files or reloading services
  -verbose          with --dry-run: print a unified diff for each file that would change
```

**Daemon mode** (default): polls NetBox on the configured `interval` and syncs continuously.

**Cron mode**: run with `--once` and schedule via cron. Exits after a single sync.

**Before making changes**: use `--dry-run` to preview what would be written without touching anything on disk.

```sh
dnstonetbox --config /etc/dnstonetbox/config.yaml --dry-run --verbose
```

## Configuration

Copy `config.example.yaml` to `config.yaml` (it is gitignored) and edit as needed. Any top-level section (`nsd`, `unbound`, `dhcpd`) can be omitted entirely to skip that generator.

### Top level

| Field | Type | Description |
|---|---|---|
| `interval` | duration | Poll interval in daemon mode (e.g. `5m`, `1h`). Default: `5m`. Ignored with `--once`. |

### `netbox`

| Field | Required | Description |
|---|---|---|
| `url` | yes | Base URL of your NetBox instance, no trailing slash (e.g. `https://netbox.example.com`) |
| `token` | yes | NetBox API token with read access to IPAM |
| `tag` | no | If set, only syncs IP addresses tagged with this value in NetBox |

### `nsd`

Generates zone files for [NSD](https://www.nlnetlabs.nl/projects/nsd/). Forward zones produce `A`/`AAAA` records; reverse zones (`.in-addr.arpa` / `.ip6.arpa`) produce `PTR` records automatically.

| Field | Required | Description |
|---|---|---|
| `zones_dir` | yes | Directory where zone files are written (e.g. `/var/nsd/zones/master`) |
| `reload_cmd` | no | Shell command to reload NSD after a zone changes (e.g. `nsd-control reload`) |
| `zones` | yes | List of zone configs (see below) |

**Zone config:**

| Field | Required | Description |
|---|---|---|
| `name` | yes | Zone apex, e.g. `example.com` or `1.168.192.in-addr.arpa` |
| `ttl` | no | Default TTL in seconds. Default: `3600` |
| `primary_ns` | yes | Primary NS FQDN with trailing dot, e.g. `ns1.example.com.` |
| `ns` | yes | List of NS FQDNs with trailing dots |
| `email` | yes | SOA rname with trailing dot, e.g. `hostmaster.example.com.` |

**Reverse zones:**

IPv4 reverse zones use the standard nibble-reversed format: `1.168.192.in-addr.arpa` covers `192.168.1.0/24`.

IPv6 reverse zones use the nibble-reversed prefix + `.ip6.arpa`. For example, `2001:db8:100::/48` becomes `0.0.1.0.8.b.d.0.1.0.0.2.ip6.arpa`.

**Zone serial:**

The serial is a 32-bit FNV-1a hash of the zone name and record content. The same host data always produces the same serial, making zone files idempotent. Note: serials are not monotonically increasing — acceptable for primary-only setups, but may cause issues with zone transfer secondaries.

### `unbound`

Generates a `local-data` include file for [Unbound](https://www.nlnetlabs.nl/projects/unbound/). Produces both forward (`A`/`AAAA`) and reverse (`PTR`) entries.

| Field | Required | Description |
|---|---|---|
| `config_file` | yes | Path to the generated include file |
| `reload_cmd` | no | Shell command to reload Unbound after a change (e.g. `unbound-control reload`) |
| `ttl` | no | TTL for all records. Default: `3600` |

Add to `unbound.conf`:
```
include: /var/unbound/etc/local-hosts.conf
```

### `dhcpd`

Generates static host declarations for OpenBSD [dhcpd(8)](https://man.openbsd.org/dhcpd.8). Only hosts with both a MAC address and an IPv4 address are emitted. The MAC address is read from the NetBox interface linked to the IP (`assigned_object.mac_address`).

| Field | Required | Description |
|---|---|---|
| `config_file` | yes | Path to the generated include file |
| `reload_cmd` | no | Shell command to reload dhcpd after a change (e.g. `rcctl restart dhcpd`) |

Add to `dhcpd.conf`:
```
include "/etc/dhcpd.d/static-hosts.conf";
```

### Full example

```yaml
interval: 5m

netbox:
  url: https://netbox.example.com
  token: your-api-token-here
  # tag: dns-sync

nsd:
  zones_dir: /var/nsd/zones/master
  reload_cmd: nsd-control reload
  zones:
    - name: example.com
      ttl: 3600
      primary_ns: ns1.example.com.
      ns:
        - ns1.example.com.
        - ns2.example.com.
      email: hostmaster.example.com.

    - name: 1.168.192.in-addr.arpa
      ttl: 3600
      primary_ns: ns1.example.com.
      ns:
        - ns1.example.com.
        - ns2.example.com.
      email: hostmaster.example.com.

unbound:
  config_file: /var/unbound/etc/local-hosts.conf
  reload_cmd: unbound-control reload

dhcpd:
  config_file: /etc/dhcpd.d/static-hosts.conf
  reload_cmd: rcctl restart dhcpd
```

## OpenBSD deployment

Install Go and build the binary on your workstation, then copy it over:

```sh
make dist
scp dist/dnstonetbox-openbsd-amd64 root@myserver:/usr/local/bin/dnstonetbox
```

The binary must run as a user with:
- Write access to the NSD zones directory, Unbound config directory, and DHCPD config directory
- Permission to run `nsd-control`, `unbound-control`, and `rcctl`

**Cron (recommended):** add to root's crontab with `--once`:

```
*/5 * * * * /usr/local/bin/dnstonetbox --config /etc/dnstonetbox/config.yaml --once
```

**Daemon:** run as a service via a custom `rc.d` script, without `--once`.

## Development

```sh
go mod tidy          # fetch deps, update go.sum
make build           # build ./dnstonetbox
make test            # go test -race -count=1 ./...
make vet             # go vet ./...
make lint            # vet + staticcheck
```
