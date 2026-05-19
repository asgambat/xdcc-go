package ircmanager

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// randomSuffix returns a random alphanumeric string of length n.
func randomSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[idx.Int64()]
	}
	return string(b)
}

// isOwnNick checks if the given nick matches the client's nick (case-insensitive).
func isOwnNick(nick, ownNick string) bool {
	return strings.EqualFold(nick, ownNick)
}

// normalizeChannel lowercases and ensures a leading '#'.
func normalizeChannel(ch string) string {
	ch = strings.ToLower(strings.TrimSpace(ch))
	if !strings.HasPrefix(ch, "#") {
		ch = "#" + ch
	}
	return ch
}

// randN returns a random integer in [0, n).
func randN(n int) int {
	if n <= 0 {
		return 0
	}
	r, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(r.Int64())
}

// ServerAddr formats address:port as a string.
func ServerAddr(address string, port int) string {
	return fmt.Sprintf("%s:%d", address, port)
}
