package queue

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"xdcc-go/internal/entities"
	xdccirc "xdcc-go/internal/irc"
	"xdcc-go/internal/store"
)

// ---------------------------------------------------------------------------
// Worker configuration
// ---------------------------------------------------------------------------

// DownloadConfig holds the configuration for a single download worker.
type DownloadConfig struct {
	TempDir        string
	DestDir        string
	ConflictPolicy string
	MaxRateBPS     int64
	Nickname       string
	Logger         xdccirc.Logger
	
	// IRCManager for persistent connections (optional - if nil, uses temporary connections)
	IRCManager IRCManagerInterface
}

// ---------------------------------------------------------------------------
// Worker result
// ---------------------------------------------------------------------------

// workerResult holds the outcome of a single download execution.
type workerResult struct {
	DownloadID  int64
	Error       error
	FilePath    string // final file path on success
	FileSize    int64
	BotNotice   string
	Skipped     bool   // true when file was skipped due to conflict policy
}

// ---------------------------------------------------------------------------
// runDownload — executes a single pack download
// ---------------------------------------------------------------------------

// runDownload runs a single XDCC pack download in the foreground (blocking).
// It:
//   - Builds an entities.XDCCPack from the download record
//   - Creates an internal/irc client with a progress callback
//   - Downloads the pack to the temp directory
//   - On success, moves the file to the destination directory
//   - Handles file conflict policy (skip/overwrite/rename)
//   - Reports progress and completion via callbacks
func runDownload(
	ctx context.Context,
	rec store.DownloadRecord,
	cfg DownloadConfig,
	progressFn func(bytesReceived, totalBytes int64, speedBPS float64),
	completeFn func(result workerResult),
) {
	logger := cfg.Logger

	result := workerResult{
		DownloadID: rec.ID,
		FileSize:   rec.FileSize,
	}

	// --- Build pack ---
	server := entities.NewIrcServerWithPort(rec.ServerAddress, 6667)
	packNumber := entities.ExtractPackNumber(rec.PackMessage)
	pack := entities.NewXDCCPack(server, rec.Bot, packNumber)
	pack.SetFilename(rec.Filename, true)
	pack.SetSize(rec.FileSize)

	// --- Resolve channels ---
	// Use the channel from the download record as the WHOIS fallback channel.
	// If empty, WHOIS will discover the bot's channel(s) automatically.
	channel := rec.Channel
	if channel != "" && channel[0] != '#' {
		channel = "#" + channel
	}

	// --- Prepare download ---
	pack.SetDirectory(cfg.TempDir)

	var srcPath string
	var downloadErr error

	// --- Execute download with IRCManager (persistent) or temp connection ---
	if cfg.IRCManager != nil {
		// Use persistent IRC connections via IRCManager
		logger.Printf("→ Using persistent IRC connection for %s", rec.ServerAddress)
		srcPath, downloadErr = cfg.IRCManager.DownloadPack(ctx, pack, channel, progressFn)
	} else {
		// Fallback to temporary IRC connection (for CLI tools)
		logger.Printf("→ Using temporary IRC connection for %s", rec.ServerAddress)
		srcPath, downloadErr = downloadWithTempConnection(ctx, pack, channel, cfg, progressFn)
	}

	if downloadErr != nil {
		result.Error = downloadErr
		result.BotNotice = "" // TODO: extract from error if available
		completeFn(result)
		return
	}

	// Verify the file exists
	if _, err := os.Stat(srcPath); err != nil {
		result.Error = fmt.Errorf("downloaded file not found at %s: %w", srcPath, err)
		completeFn(result)
		return
	}

	// --- Move to destination directory ---
	destPath := filepath.Join(cfg.DestDir, rec.Filename)

	// Handle conflict policy
	conflictPolicy := cfg.ConflictPolicy
	if conflictPolicy == "" {
		conflictPolicy = "skip"
	}

	if _, err := os.Stat(destPath); err == nil {
		// File already exists at destination
		switch conflictPolicy {
	case "skip":
		// Remove the temp file and report as skipped
		os.Remove(srcPath)
		result.FilePath = destPath
		result.Skipped = true
		completeFn(result)
		return
		case "overwrite":
			// Remove destination file, then move
			if err := os.Remove(destPath); err != nil {
				result.Error = fmt.Errorf("cannot overwrite %s: %w", destPath, err)
				completeFn(result)
				return
			}
		case "rename":
			// Add timestamp suffix
			ext := filepath.Ext(destPath)
			base := destPath[:len(destPath)-len(ext)]
			destPath = fmt.Sprintf("%s_%s%s", base, time.Now().Format("20060102_150405"), ext)
		}
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		result.Error = fmt.Errorf("creating destination directory: %w", err)
		completeFn(result)
		return
	}

	// Move the file (rename works within same filesystem; fall back to copy+delete)
	if err := os.Rename(srcPath, destPath); err != nil {
		// Cross-filesystem move: copy then delete
		if err := copyFile(srcPath, destPath); err != nil {
			result.Error = fmt.Errorf("moving file to destination: %w", err)
			completeFn(result)
			return
		}
		os.Remove(srcPath)
	}

	result.FilePath = destPath
	result.Error = nil

	// Update result file size from actual downloaded file
	if fi, err := os.Stat(destPath); err == nil {
		result.FileSize = fi.Size()
	}

	completeFn(result)
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stating source: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("creating destination: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}
	return nil
}

// downloadWithTempConnection performs a download using a temporary IRC connection.
// This is used as fallback when IRCManager is not available (e.g., CLI tools).
func downloadWithTempConnection(
	ctx context.Context,
	pack *entities.XDCCPack,
	channel string,
	cfg DownloadConfig,
	progressFn func(bytesReceived, totalBytes int64, speedBPS float64),
) (string, error) {
	logger := cfg.Logger

	// Determine throttle (BPS limit)
	throttle := cfg.MaxRateBPS
	if throttle < 0 {
		throttle = 0
	}

	opts := xdccirc.DownloadOptions{
		ConnectTimeout:   120,
		StallTimeout:     60,
		FallbackChannel:  channel,
		ThrottleBytes:    throttle,
		WaitTime:         1,
		ChannelJoinDelay: -1, // random 5-10s
		Username:         cfg.Nickname,
		Logger:           logger,
		ProgressCallback: progressFn,
	}

	// Execute download
	packSlice := []*entities.XDCCPack{pack}
	client := xdccirc.NewClient(ctx, packSlice, opts, -1) // -1 = quiet
	results := client.DownloadAll()

	if len(results) == 0 {
		return "", fmt.Errorf("no result from download client")
	}

	r := results[0]
	if r.Error != nil {
		return "", r.Error
	}

	// Return downloaded file path
	srcPath := pack.GetFilepath()
	if r.FilePath != "" {
		srcPath = r.FilePath
	}

	return srcPath, nil
}
