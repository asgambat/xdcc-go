package downloader

import (
	"testing"

	"xdcc-go/internal/entities"
)

func TestGroupByServer_Empty(t *testing.T) {
	groups := groupByServer(nil)
	if groups != nil {
		t.Errorf("expected nil, got %v", groups)
	}
}

func TestGroupByServer_SinglePack(t *testing.T) {
	p := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1)
	groups := groupByServer([]*entities.XDCCPack{p})
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Errorf("expected 1 group with 1 pack, got %v", groups)
	}
}

func TestGroupByServer_SameServer(t *testing.T) {
	srv := entities.NewIrcServer("irc.rizon.net")
	packs := []*entities.XDCCPack{
		entities.NewXDCCPack(srv, "BotA", 1),
		entities.NewXDCCPack(srv, "BotB", 2),
		entities.NewXDCCPack(srv, "BotC", 3),
	}
	groups := groupByServer(packs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Errorf("expected 3 packs in group, got %d", len(groups[0]))
	}
}

func TestGroupByServer_DifferentServers(t *testing.T) {
	packs := []*entities.XDCCPack{
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1),
		entities.NewXDCCPack(entities.NewIrcServer("irc.other.net"), "Bot", 2),
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 3),
	}
	groups := groupByServer(packs)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups (consecutive grouping), got %d", len(groups))
	}
}

func TestGroupByServer_ConsecutiveSameServer(t *testing.T) {
	packs := []*entities.XDCCPack{
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 1),
		entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "Bot", 2),
		entities.NewXDCCPack(entities.NewIrcServer("irc.other.net"), "Bot", 3),
		entities.NewXDCCPack(entities.NewIrcServer("irc.other.net"), "Bot", 4),
	}
	groups := groupByServer(packs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(groups[0]) != 2 || len(groups[1]) != 2 {
		t.Errorf("expected [2, 2] packs, got [%d, %d]", len(groups[0]), len(groups[1]))
	}
}
