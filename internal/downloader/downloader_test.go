package downloader

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"xdcc-go/internal/entities"
	xdccirc "xdcc-go/internal/irc"
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

// captureStdout captures everything written to os.Stdout during fn.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func TestPrintResult_Success(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{Error: nil})
	})
	if out != "" {
		t.Errorf("expected no output for success, got %q", out)
	}
}

func TestPrintResult_AlreadyDownloaded(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	pack.SetFilename("movie.mkv", true)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "already_downloaded", Message: "file already downloaded"},
		})
	})
	if !strings.Contains(out, "already downloaded") {
		t.Errorf("expected 'already downloaded' in output, got %q", out)
	}
	if !strings.Contains(out, "movie.mkv") {
		t.Errorf("expected filename in output, got %q", out)
	}
}

func TestPrintResult_BotDenied_WithNotice(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error:         &xdccirc.XDCCDownloadError{Kind: "bot_denied", Message: "denied"},
			LastBotNotice: "You have been denied access",
		})
	})
	if !strings.Contains(out, "You have been denied access") {
		t.Errorf("expected LastBotNotice in output, got %q", out)
	}
}

func TestPrintResult_BotDenied_NoNotice(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	pack.SetFilename("episode.mp4", true)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "bot_denied", Message: "denied"},
		})
	})
	if !strings.Contains(out, "denied") {
		t.Errorf("expected 'denied' in output, got %q", out)
	}
	if !strings.Contains(out, "episode.mp4") {
		t.Errorf("expected filename in output, got %q", out)
	}
}

func TestPrintResult_BotNotFound(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "MissingBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "bot_not_found", Message: "bot does not exist on server"},
		})
	})
	if !strings.Contains(out, "MissingBot") {
		t.Errorf("expected bot name in output, got %q", out)
	}
	if !strings.Contains(out, "irc.rizon.net") {
		t.Errorf("expected server address in output, got %q", out)
	}
}

func TestPrintResult_ServerUnreachable(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.dead.net"), "TestBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "server_unreachable", Message: "connection refused"},
		})
	})
	if !strings.Contains(out, "irc.dead.net") {
		t.Errorf("expected server address in output, got %q", out)
	}
	if !strings.Contains(out, "--server") {
		t.Errorf("expected '--server' tip in output, got %q", out)
	}
}

func TestPrintResult_Unrecoverable(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "unrecoverable", Message: "IP banned"},
		})
	})
	if !strings.Contains(out, "Unrecoverable") {
		t.Errorf("expected 'Unrecoverable' in output, got %q", out)
	}
}

func TestPrintResult_Cancelled(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "cancelled", Message: "cancelled by user"},
		})
	})
	if !strings.Contains(out, "cancelled") {
		t.Errorf("expected 'cancelled' in output, got %q", out)
	}
}

func TestPrintResult_Timeout(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 42)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "timeout", Message: "download timed out"},
		})
	})
	if !strings.Contains(out, "42") {
		t.Errorf("expected pack number in output, got %q", out)
	}
}

func TestPrintResult_DownloadFailed(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 1)
	pack.SetFilename("bigfile.iso", true)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: &xdccirc.XDCCDownloadError{Kind: "download_failed", Message: "download did not complete"},
		})
	})
	if !strings.Contains(out, "bigfile.iso") {
		t.Errorf("expected filename in output, got %q", out)
	}
}

func TestPrintResult_UnknownError(t *testing.T) {
	pack := entities.NewXDCCPack(entities.NewIrcServer("irc.rizon.net"), "TestBot", 99)
	out := captureStdout(func() {
		printResult(pack, xdccirc.PackResult{
			Error: errors.New("something unexpected"),
		})
	})
	if !strings.Contains(out, "99") {
		t.Errorf("expected pack number in output, got %q", out)
	}
	if !strings.Contains(out, "something unexpected") {
		t.Errorf("expected error message in output, got %q", out)
	}
}
