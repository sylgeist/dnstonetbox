package main

import (
	"flag"
	"log"
	"time"

	"github.com/sseekamp/dnstonetbox/dhcpd"
	"github.com/sseekamp/dnstonetbox/netbox"
	"github.com/sseekamp/dnstonetbox/nsd"
	"github.com/sseekamp/dnstonetbox/unbound"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	once := flag.Bool("once", false, "run once and exit (useful for cron)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	client := netbox.NewClient(cfg.Netbox)

	sync := func() {
		hosts, err := client.FetchHosts()
		if err != nil {
			log.Printf("netbox: fetch error: %v", err)
			return
		}
		log.Printf("netbox: fetched %d hosts", len(hosts))

		if err := nsd.Sync(cfg.NSD, hosts); err != nil {
			log.Printf("nsd: %v", err)
		}
		if err := unbound.Sync(cfg.Unbound, hosts); err != nil {
			log.Printf("unbound: %v", err)
		}
		if err := dhcpd.Sync(cfg.DHCPD, hosts); err != nil {
			log.Printf("dhcpd: %v", err)
		}
	}

	sync()
	if *once {
		return
	}

	interval := cfg.Interval.Duration
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	log.Printf("polling every %s (use --once to run once)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		sync()
	}
}
