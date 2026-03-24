package netbox

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/sylgeist/dnstonetbox/model"
)

// Config holds NetBox connection settings.
type Config struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	Tag   string `yaml:"tag"` // optional: filter to IPs tagged with this value
}

// Client is a minimal NetBox REST API client.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient creates a new Client.
func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{}}
}

// FetchHosts retrieves all IP addresses with a dns_name set and returns them
// as deduplicated Host records, merging IPv4 and IPv6 entries by hostname.
func (c *Client) FetchHosts() ([]model.Host, error) {
	endpoint := c.cfg.URL + "/api/ipam/ip-addresses/"

	params := url.Values{}
	params.Set("limit", "1000")
	params.Set("dns_name__n", "") // exclude blank dns_name
	if c.cfg.Tag != "" {
		params.Set("tag", c.cfg.Tag)
	}

	byName := make(map[string]*model.Host)
	nextURL := endpoint + "?" + params.Encode()

	for nextURL != "" {
		page, err := c.fetchPage(nextURL)
		if err != nil {
			return nil, err
		}

		for _, entry := range page.Results {
			if entry.DNSName == "" {
				continue
			}
			ip, _, err := net.ParseCIDR(entry.Address)
			if err != nil {
				continue
			}

			h, ok := byName[entry.DNSName]
			if !ok {
				h = &model.Host{Name: entry.DNSName}
				byName[entry.DNSName] = h
			}

			if v4 := ip.To4(); v4 != nil {
				h.IPv4 = v4
			} else {
				h.IPv6 = ip
			}

			if h.MAC == "" && entry.AssignedObject != nil && entry.AssignedObject.MACAddress != "" {
				h.MAC = entry.AssignedObject.MACAddress
			}
		}

		nextURL = ""
		if page.Next != nil {
			nextURL = *page.Next
		}
	}

	hosts := make([]model.Host, 0, len(byName))
	for _, h := range byName {
		hosts = append(hosts, *h)
	}
	return hosts, nil
}

// ipListResponse is the paginated NetBox response envelope.
type ipListResponse struct {
	Count   int       `json:"count"`
	Next    *string   `json:"next"`
	Results []ipEntry `json:"results"`
}

// ipEntry maps the fields we care about from /api/ipam/ip-addresses/.
type ipEntry struct {
	Address        string          `json:"address"`
	DNSName        string          `json:"dns_name"`
	AssignedObject *assignedObject `json:"assigned_object"`
}

// assignedObject is the nested interface representation.
// Both dcim.interface and virtualization.vminterface expose mac_address.
type assignedObject struct {
	MACAddress string `json:"mac_address"`
}

func (c *Client) fetchPage(rawURL string) (*ipListResponse, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("netbox: build request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.cfg.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netbox: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("netbox: unexpected status %d", resp.StatusCode)
	}

	var result ipListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("netbox: decode response: %w", err)
	}
	return &result, nil
}
