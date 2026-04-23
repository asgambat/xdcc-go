package irc

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

func (c *Client) checkServerReachable(host string) error {
	addrs, err := net.LookupHost(host)
	if err != nil {
		c.noticef("DNS resolution failed for %s: %v", host, err)
		return fmt.Errorf("%w: cannot resolve %s: %v", ErrServerUnreachable, host, err)
	}
	c.debugf("DNS resolved %s → %v", host, addrs)
	for _, addr := range addrs {
		if addr == "0.0.0.0" || addr == "::" {
			c.noticef("Server %s resolves to %s — DNS-blocked or server is down", host, addr)
			return fmt.Errorf("%w: %s resolves to %s (DNS-blocked or server down)",
				ErrServerUnreachable, host, addr)
		}
	}
	return nil
}

func isConnectError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, k := range []string{
		"connection refused", "no route to host", "network is unreachable",
		"i/o timeout", "no such host", "dial ",
	} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func randomUsername() string {
	firstNames := []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"}
	lastNames := []string{"Smith", "Jones", "Brown", "Wilson", "Taylor", "Davis", "Clark", "Lewis"}
	n1, _ := rand.Int(rand.Reader, big.NewInt(int64(len(firstNames))))
	n2, _ := rand.Int(rand.Reader, big.NewInt(int64(len(lastNames))))
	num, _ := rand.Int(rand.Reader, big.NewInt(90))
	return fmt.Sprintf("%s%s%d%s",
		firstNames[n1.Int64()], lastNames[n2.Int64()], num.Int64()+10, randomSuffix(3))
}

func randomSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[idx.Int64()]
	}
	return string(b)
}

func formatDuration(d time.Duration) string {
	if d < 90*time.Second {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func ipNumToQuad(ipNum string) string {
	n := parseU32(ipNum)
	return fmt.Sprintf("%d.%d.%d.%d",
		(n>>24)&0xFF, (n>>16)&0xFF, (n>>8)&0xFF, n&0xFF)
}

func parseI64(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func parseU32(s string) uint32 {
	var v uint32
	fmt.Sscanf(s, "%d", &v)
	return v
}

func randN(n int) int {
	r, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(r.Int64())
}

// splitDCC splits a DCC message text, respecting quoted filenames.
func splitDCC(s string) []string {
	var parts []string
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		if s[0] == '"' {
			end := strings.Index(s[1:], "\"")
			if end < 0 {
				parts = append(parts, s[1:])
				break
			}
			parts = append(parts, s[1:end+1])
			s = strings.TrimSpace(s[end+2:])
		} else {
			sp := strings.IndexByte(s, ' ')
			if sp < 0 {
				parts = append(parts, s)
				break
			}
			parts = append(parts, s[:sp])
			s = strings.TrimSpace(s[sp+1:])
		}
	}
	return parts
}
