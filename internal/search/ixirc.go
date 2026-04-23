package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"xdcc-go/internal/entities"
)

// IxircEngine searches for XDCC packs on ixirc.com using their JSON API.
type IxircEngine struct{}

func (e *IxircEngine) Name() string { return "ixirc" }

type ixircResponse struct {
	PageCount int           `json:"pc"`
	Results   []ixircResult `json:"results"`
}

type ixircResult struct {
	Uname string  `json:"uname"` // bot nick (absent if offline)
	Naddr string  `json:"naddr"` // IRC server address
	Nport int     `json:"nport"` // IRC server port
	N     int     `json:"n"`     // pack number
	Name  string  `json:"name"`  // filename
	Sz    float64 `json:"sz"`    // size in bytes
}

func (e *IxircEngine) Search(term string) ([]*entities.XDCCPack, error) {
	if term == "" {
		return nil, nil
	}

	var packs []*entities.XDCCPack
	pageID := 0

	for {
		apiURL := fmt.Sprintf("https://ixirc.com/api/?q=%s&pn=%d",
			url.QueryEscape(term), pageID)

		resp, err := http.Get(apiURL)
		if err != nil {
			return packs, fmt.Errorf("ixirc request failed: %w", err)
		}

		var data ixircResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			return packs, fmt.Errorf("ixirc JSON decode failed: %w", err)
		}
		resp.Body.Close()

		for _, result := range data.Results {
			if result.Uname == "" {
				continue // bot not online
			}
			port := result.Nport
			if port == 0 {
				port = 6667
			}
			server := entities.NewIrcServerWithPort(result.Naddr, port)
			pack := entities.NewXDCCPack(server, result.Uname, result.N)
			pack.SetFilename(result.Name, true)
			pack.SetSize(int64(result.Sz))
			packs = append(packs, pack)
		}

		pageID++
		if pageID >= data.PageCount {
			break
		}
	}

	return packs, nil
}
