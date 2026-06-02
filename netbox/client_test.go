package netbox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sylgeist/dnstonetbox/netbox"
)

// response is a helper for building mock NetBox API responses.
type response struct {
	Count   int         `json:"count"`
	Next    *string     `json:"next"`
	Results []ipFixture `json:"results"`
}

type ipFixture struct {
	Address        string      `json:"address"`
	DNSName        string      `json:"dns_name"`
	AssignedObject *macFixture `json:"assigned_object"`
}

type macFixture struct {
	MACAddress string `json:"mac_address"`
}

func singlePageServer(t *testing.T, results []ipFixture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response{Count: len(results), Results: results}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
}

func TestFetchHosts_MergesIPv4AndIPv6(t *testing.T) {
	srv := singlePageServer(t, []ipFixture{
		{Address: "192.168.1.10/24", DNSName: "host1.example.com",
			AssignedObject: &macFixture{MACAddress: "aa:bb:cc:dd:ee:ff"}},
		{Address: "2001:db8::1/64", DNSName: "host1.example.com"},
	})
	defer srv.Close()

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("got %d hosts, want 1 (merged)", len(hosts))
	}
	h := hosts[0]
	if h.Name != "host1.example.com" {
		t.Errorf("Name = %q, want host1.example.com", h.Name)
	}
	if h.IPv4 == nil || h.IPv4.String() != "192.168.1.10" {
		t.Errorf("IPv4 = %v, want 192.168.1.10", h.IPv4)
	}
	if h.IPv6 == nil || h.IPv6.String() != "2001:db8::1" {
		t.Errorf("IPv6 = %v, want 2001:db8::1", h.IPv6)
	}
	if h.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC = %q, want aa:bb:cc:dd:ee:ff", h.MAC)
	}
}

func TestFetchHosts_SkipsBlankDNSName(t *testing.T) {
	srv := singlePageServer(t, []ipFixture{
		{Address: "192.168.1.10/24", DNSName: ""},
		{Address: "192.168.1.11/24", DNSName: "host2.example.com"},
	})
	defer srv.Close()

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("got %d hosts, want 1 (blank dns_name skipped)", len(hosts))
	}
	if hosts[0].Name != "host2.example.com" {
		t.Errorf("Name = %q, want host2.example.com", hosts[0].Name)
	}
}

func TestFetchHosts_Pagination(t *testing.T) {
	// Set up after server creation so the URL is available inside the handler.
	var srvURL string
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page++
		var resp response
		if page == 1 {
			next := srvURL + "/api/ipam/ip-addresses/?offset=1"
			resp = response{Count: 2, Next: &next, Results: []ipFixture{
				{Address: "192.168.1.10/24", DNSName: "host1.example.com"},
			}}
		} else {
			resp = response{Count: 2, Results: []ipFixture{
				{Address: "192.168.1.11/24", DNSName: "host2.example.com"},
			}}
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2 (across two pages)", len(hosts))
	}
}

func TestFetchHosts_NilAssignedObject(t *testing.T) {
	srv := singlePageServer(t, []ipFixture{
		{Address: "192.168.1.10/24", DNSName: "host1.example.com", AssignedObject: nil},
	})
	defer srv.Close()

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("got %d hosts, want 1", len(hosts))
	}
	if hosts[0].MAC != "" {
		t.Errorf("MAC = %q, want empty when assigned_object is nil", hosts[0].MAC)
	}
}

func TestFetchHosts_SortedByName(t *testing.T) {
	// Supplied in reverse order; FetchHosts must return them sorted by Name so
	// that generated output is stable across runs (map iteration is random).
	srv := singlePageServer(t, []ipFixture{
		{Address: "192.168.1.13/24", DNSName: "delta.example.com"},
		{Address: "192.168.1.12/24", DNSName: "charlie.example.com"},
		{Address: "192.168.1.11/24", DNSName: "bravo.example.com"},
		{Address: "192.168.1.10/24", DNSName: "alpha.example.com"},
	})
	defer srv.Close()

	hosts, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	want := []string{"alpha.example.com", "bravo.example.com", "charlie.example.com", "delta.example.com"}
	if len(hosts) != len(want) {
		t.Fatalf("got %d hosts, want %d", len(hosts), len(want))
	}
	for i, name := range want {
		if hosts[i].Name != name {
			t.Errorf("hosts[%d].Name = %q, want %q (not sorted)", i, hosts[i].Name, name)
		}
	}
}

func TestFetchHosts_TrailingSlashURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	// Configured URL has a trailing slash; the request path must not double up.
	_, err := netbox.NewClient(netbox.Config{URL: srv.URL + "/", Token: "test"}).FetchHosts()
	if err != nil {
		t.Fatalf("FetchHosts: %v", err)
	}
	if gotPath != "/api/ipam/ip-addresses/" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/ipam/ip-addresses/")
	}
}

func TestFetchHosts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := netbox.NewClient(netbox.Config{URL: srv.URL, Token: "bad"}).FetchHosts()
	if err == nil {
		t.Fatal("expected error on 401 response, got nil")
	}
}

func TestFetchHosts_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	netbox.NewClient(netbox.Config{URL: srv.URL, Token: "mytoken"}).FetchHosts() //nolint:errcheck
	if gotAuth != "Token mytoken" {
		t.Errorf("Authorization header = %q, want \"Token mytoken\"", gotAuth)
	}
}
