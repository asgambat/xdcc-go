package search

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"xdcc-go/internal/entities"
)

// XdccEuEngine searches for XDCC packs on xdcc.eu.
// Supports pipe-separated filter syntax: t=<ext>|b=<bot>|q=<query>
type XdccEuEngine struct {
	Verbose bool
}

func (e *XdccEuEngine) Name() string { return "xdcc-eu" }

func (e *XdccEuEngine) Search(term string) ([]*entities.XDCCPack, error) {
	typeFilter := ""
	botFilter := ""
	query := term

	if strings.Contains(term, "|") {
		tokens := strings.Split(term, "|")
		query = getParam("q", tokens)
		typeFilter = getParam("t", tokens)
		botFilter = getParam("b", tokens)
	}

	searchURL := fmt.Sprintf("https://www.xdcc.eu/search.php?searchkey=%s",
		url.QueryEscape(query))

	if e.Verbose {
		fmt.Printf("[DEBUG] GET %s\n", searchURL)
	}

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("xdcc.eu request failed: %w", err)
	}
	defer resp.Body.Close()

	if e.Verbose {
		fmt.Printf("[DEBUG] HTTP status: %s\n", resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xdcc.eu HTML parse failed: %w", err)
	}

	if e.Verbose {
		rescount := strings.TrimSpace(doc.Find(".rescount").Text())
		if rescount != "" {
			fmt.Printf("[DEBUG] Page says: %s\n", rescount)
		}
		fmt.Printf("[DEBUG] Table rows found: %d\n", doc.Find("tbody tr").Length())
	}

	var packs []*entities.XDCCPack
	skipped := 0

	doc.Find("tbody tr").Each(func(i int, row *goquery.Selection) {
		parts := row.Find("td")
		if parts.Length() < 7 {
			skipped++
			return
		}

		// The info link (with data-s and data-p) is identified by the data-s attribute.
		// td[1] contains: connect link, info link (data-s/data-p), delete link.
		link := parts.Eq(1).Find("a[data-s]")
		serverAddr, exists := link.Attr("data-s")
		if !exists {
			if e.Verbose {
				fmt.Printf("[DEBUG] Row %d: no data-s, skipping\n", i)
			}
			skipped++
			return
		}
		packMsg, exists := link.Attr("data-p")
		if !exists {
			skipped++
			return
		}

		// packMsg format: "<bot> xdcc send #<num>"
		msgParts := strings.SplitN(packMsg, " xdcc send #", 2)
		if len(msgParts) != 2 {
			if e.Verbose {
				fmt.Printf("[DEBUG] Row %d: unexpected data-p format: %q\n", i, packMsg)
			}
			skipped++
			return
		}
		bot := strings.TrimSpace(msgParts[0])
		var packNum int
		fmt.Sscanf(msgParts[1], "%d", &packNum)
		if packNum == 0 {
			skipped++
			return
		}

		sizeRaw := strings.TrimSpace(parts.Eq(5).Text())
		filename := strings.TrimSpace(parts.Eq(6).Text())
		size := entities.ByteStringToByteCount(extractNumericSuffix(sizeRaw))

		if e.Verbose {
			fmt.Printf("[DEBUG] Row %d: bot=%s pack=#%d size=%s file=%s\n", i, bot, packNum, sizeRaw, filename)
		}

		if typeFilter != "" && !checkFileType(typeFilter, filename) {
			skipped++
			return
		}
		if botFilter != "" && !strings.Contains(strings.ToLower(bot), strings.ToLower(botFilter)) {
			skipped++
			return
		}

		server := entities.NewIrcServer(serverAddr)
		pack := entities.NewXDCCPack(server, bot, packNum)
		pack.SetSize(size)
		pack.SetFilename(filename, true)
		packs = append(packs, pack)
	})

	if e.Verbose && skipped > 0 {
		fmt.Printf("[DEBUG] Skipped %d rows\n", skipped)
	}

	return packs, nil
}

// getParam extracts the value of a key=value token from a slice of tokens.
func getParam(key string, tokens []string) string {
	prefix := key + "="
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			return t[len(prefix):]
		}
	}
	return ""
}

// checkFileType returns true if the filename matches the type filter.
// "v" matches video files (mkv, avi, mp4).
func checkFileType(typeFilter, filename string) bool {
	if typeFilter == "v" {
		lower := strings.ToLower(filename)
		return strings.HasSuffix(lower, ".mkv") ||
			strings.HasSuffix(lower, ".avi") ||
			strings.HasSuffix(lower, ".mp4")
	}
	return true
}

// extractNumericSuffix extracts the numeric+unit part of a size string
// by stripping non-numeric leading characters.
func extractNumericSuffix(s string) string {
	s = strings.TrimSpace(s)
	for i, ch := range s {
		if ch >= '0' && ch <= '9' {
			return s[i:]
		}
	}
	return s
}
