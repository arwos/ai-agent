/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// externalHTTPClient is for user-supplied URLs. It never uses environment
// proxies and rejects addresses that can reach local or private services.
func externalHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: nil, DialContext: dialExternal}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func validateExternalURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("URL must be an absolute http or https URL without credentials")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicAddress(ip) {
		return fmt.Errorf("URL must not target a local or private address")
	}
	return nil
}

func dialExternal(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{}
	for _, ip := range ips {
		if !publicAddress(ip) {
			continue
		}
		if conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port)); dialErr == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("URL host does not resolve to a public address")
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}
