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
		json.NewEncoder(w).Encode(response{Count: len(results), Results: results})
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
		if page == 1 {
			next := srvURL + "/api/ipam/ip-addresses/?offset=1"
			json.NewEncoder(w).Encode(response{
				Count: 2,
				Next:  &next,
				Results: []ipFixture{
					{Address: "192.168.1.10/24", DNSName: "host1.example.com"},
				},
			})
		} else {
			json.NewEncoder(w).Encode(response{
				Count: 2,
				Results: []ipFixture{
					{Address: "192.168.1.11/24", DNSName: "host2.example.com"},
				},
			})
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
		json.NewEncoder(w).Encode(response{})
	}))
	defer srv.Close()

	netbox.NewClient(netbox.Config{URL: srv.URL, Token: "mytoken"}).FetchHosts() //nolint:errcheck
	if gotAuth != "Token mytoken" {
		t.Errorf("Authorization header = %q, want \"Token mytoken\"", gotAuth)
	}
}
