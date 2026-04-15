package middleware

// cluster_only.go — AR8: restrict /metrics to cluster-internal IPs only.
// Private / loopback addresses are allowed; public IPs receive 403.
// Primary enforcement is Kubernetes ClusterIP + Nginx deny-all;
// this is a defence-in-depth check inside the process.

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

var clusterCIDRs = []net.IPNet{
	// RFC 1918 private ranges
	{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
	{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	// Loopback
	{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
	// IPv6 loopback / ULA
}

// ClusterOnly returns 403 for any request whose remote IP is not a private/loopback address.
func ClusterOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		ipStr := c.ClientIP()
		ip := net.ParseIP(ipStr)

		if ip == nil || !isClusterIP(ip) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

func isClusterIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	// IPv6 ULA fc00::/7
	if ip4 := ip.To4(); ip4 == nil {
		if len(ip) == 16 && (ip[0]&0xfe) == 0xfc {
			return true
		}
	}
	for _, cidr := range clusterCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
