// Package hcloud provides node discovery for Hetzner Cloud.
package hcloud

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/hetznercloud/hcloud-go/hcloud"
)

type Provider struct{}

func (p *Provider) Help() string {
	return `Hetzner Cloud:
		provider:       "hcloud"
		api_token:      The Hetzner Cloud API token to use
		location:       The Hetzner Cloud datacenter location to filter by (eg. "fsn1"). Optional. If empty, will detect the location of the current server.
										If not on an hcloud server, will connect to all servers matching label_selector.
		label_selector: The label selector to filter by
		address_type:   "private_v4", "public_v4" or "public_v6". (default: "private_v4") In the case of private networks, the first one will be used.

		Variables can also be provided by environment variables:
		export HCLOUD_LOCATION for location
		export HCLOUD_TOKEN for api_token
`
}

// serverIP returns the IP address of the specified type for the hcloud server.
func serverIP(s *hcloud.Server, addrType string, l *log.Logger) string {
	switch addrType {
	case "public_v4":
		if !s.PublicNet.IPv4.Blocked {
			l.Printf("[INFO] discover-hcloud: instance %s (%d) has public IP %s", s.Name, s.ID, s.PublicNet.IPv4.IP.String())
			return s.PublicNet.IPv4.IP.String()
		} else if len(s.PublicNet.FloatingIPs) != 0 {
			l.Printf("[INFO] discover-hcloud: public IPv4 for instance %s (%d) is blocked, checking associated floating IPs", s.Name, s.ID)
			for _, floatingIP := range s.PublicNet.FloatingIPs {
				if floatingIP.Type == hcloud.FloatingIPTypeIPv4 && !floatingIP.Blocked {
					l.Printf("[INFO] discover-hcloud: instance %s (%d) has floating IP %s", s.Name, s.ID, floatingIP.IP.String())
					return floatingIP.IP.String()
				}
			}
		}
	case "public_v6":
		if !s.PublicNet.IPv6.Blocked {
			l.Printf("[INFO] discover-hcloud: instance %s (%d) has public IP %s", s.Name, s.ID, s.PublicNet.IPv6.IP.String())
			return s.PublicNet.IPv6.IP.String()
		} else if len(s.PublicNet.FloatingIPs) != 0 {
			l.Printf("[INFO] discover-hcloud: public IPv6 for instance %s (%d) is blocked, checking associated floating IPs", s.Name, s.ID)
			for _, floatingIP := range s.PublicNet.FloatingIPs {
				if floatingIP.Type == hcloud.FloatingIPTypeIPv6 && !floatingIP.Blocked {
					l.Printf("[INFO] discover-hcloud: instance %s (%d) has floating IP %s", s.Name, s.ID, floatingIP.IP.String())
					return floatingIP.IP.String()
				}
			}
		}
	case "private_v4":
		if len(s.PrivateNet) == 0 {
			l.Printf("[INFO] discover-hcloud: instance %s (%d) has no private IP", s.Name, s.ID)
		} else {
			l.Printf("[INFO] discover-hcloud: instance %s (%d) has private IP %s", s.Name, s.ID, s.PrivateNet[0].IP.String())
			return s.PrivateNet[0].IP.String()
		}
	default:
	}

	l.Printf("[DEBUG] discover-hcloud: instance %s (%d) has no valid associated IP address", s.Name, s.ID)
	return ""
}

func (p *Provider) Addrs(args map[string]string, l *log.Logger) ([]string, error) {
	if args["provider"] != "hcloud" {
		return nil, fmt.Errorf("discover-hcloud: invalid provider %s", args["provider"])
	}

	if l == nil {
		l = log.New(ioutil.Discard, "", 0)
	}

	addressType := args["address_type"]
	location := argsOrEnv(args, "location", "HCLOUD_LOCATION")
	labelSelector := args["label_selector"]
	apiToken := argsOrEnv(args, "api_token", "HCLOUD_TOKEN")

	if apiToken == "" {
		return nil, fmt.Errorf("discover-hcloud: no API token specified")
	}

	client := getHcloudClient(apiToken)

	if location == "" {
		content, err := ioutil.ReadFile("/etc/hostname")
		if err != nil {
			return nil, fmt.Errorf("discover-hcloud: %s", err)
		}

		hostname := strings.TrimSpace(string(content))

		l.Printf("[INFO] discover-hcloud: Location not specified. Searching for current server named %s.", hostname)

		server, _, err := client.Server.GetByName(context.Background(), hostname)

		if err != nil {
			return nil, fmt.Errorf("discover-hcloud: %s", err)
		}

		if server != nil {
			l.Printf("[INFO] discover-hcloud: Detected current server %s with id %d", server.Name, server.ID)

			location = server.Datacenter.Location.Name
		} else {
			l.Printf("[INFO] discover-hcloud: No location specified and not an hcloud server. Joining all matching label selector.")
		}
	}

	if addressType == "" {
		l.Printf("[INFO] discover-hcloud: address type not provided, using 'private_v4'")
		addressType = "private_v4"
	}

	if addressType != "private_v4" && addressType != "public_v4" && addressType != "public_v6" {
		l.Printf("[INFO] discover-hcloud: address_type %s is invalid, falling back to 'private_v4'. valid values are: private_v4, public_v4, public_v6", addressType)
		addressType = "private_v4"
	}

	if location != "" {
		l.Printf("[INFO] discover-hcloud: filtering by location %s", location)
	}

	l.Printf("[DEBUG] discover-hcloud: using address_type=%s label_selector=%s location=%s", addressType, labelSelector, location)

	options := hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: labelSelector,
		},
		Status: []hcloud.ServerStatus{hcloud.ServerStatusRunning},
	}

	servers, err := client.Server.AllWithOpts(context.Background(), options)
	if err != nil {
		return nil, fmt.Errorf("discover-hcloud: %s", err)
	}

	var addrs []string
	for _, s := range servers {
		if location == "" || location == s.Datacenter.Location.Name {
			if serverIP := serverIP(s, addressType, l); serverIP != "" {
				addrs = append(addrs, serverIP)
			}
		}
	}

	log.Printf("[DEBUG] discover-hcloud: found IP addresses: %v", addrs)
	return addrs, nil
}

func getHcloudClient(apiToken string) *hcloud.Client {
	client := hcloud.NewClient(hcloud.WithToken(apiToken))
	return client
}

func argsOrEnv(args map[string]string, key, env string) string {
	if value := args[key]; value != "" {
		return value
	}
	return os.Getenv(env)
}
