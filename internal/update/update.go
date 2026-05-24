package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const (
	repoOwner = "nerveband"
	repoName  = "airvault"
	cacheFile = "update_cache.json"
)

type Cache struct {
	LastCheck      time.Time `json:"last_check"`
	LatestVersion  string    `json:"latest_version"`
	UpdateRequired bool      `json:"update_required"`
}

type CheckResult struct {
	HasUpdate     bool
	LatestVersion string
	Err           error
}

func CheckAsync(currentVersion string) <-chan CheckResult {
	ch := make(chan CheckResult, 1)
	go func() {
		hasUpdate, latestVersion, err := Check(currentVersion)
		ch <- CheckResult{HasUpdate: hasUpdate, LatestVersion: latestVersion, Err: err}
	}()
	return ch
}

func Check(currentVersion string) (bool, string, error) {
	if currentVersion == "dev" {
		return false, "", nil
	}
	if cached, err := loadCache(); err == nil && time.Since(cached.LastCheck) < 24*time.Hour {
		return cached.UpdateRequired, cached.LatestVersion, nil
	}
	updater, err := newUpdater()
	if err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil || !found {
		return false, "", err
	}
	hasUpdate := latest.GreaterThan(currentVersion)
	_ = saveCache(Cache{LastCheck: time.Now(), LatestVersion: latest.Version(), UpdateRequired: hasUpdate})
	return hasUpdate, latest.Version(), nil
}

func Upgrade(currentVersion string) (bool, error) {
	fmt.Printf("Current version: %s\n", currentVersion)
	if currentVersion == "dev" {
		fmt.Println("Running dev build. Use 'make install' from the repo to update.")
		return false, nil
	}
	updater, err := newUpdater()
	if err != nil {
		return false, err
	}
	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil {
		return false, err
	}
	if !found || latest.LessOrEqual(currentVersion) {
		fmt.Println("Already up to date")
		return false, nil
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, err
	}
	if err := checkWritePermission(exe); err != nil {
		return false, err
	}
	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return false, err
	}
	if runtime.GOOS == "darwin" {
		_ = exec.Command("codesign", "-s", "-", "-f", exe).Run()
	}
	_ = saveCache(Cache{LastCheck: time.Now(), LatestVersion: latest.Version()})
	fmt.Printf("Successfully upgraded to %s\n", latest.Version())
	return true, nil
}

func FormatNotice(result CheckResult) string {
	if result.Err != nil || !result.HasUpdate {
		return ""
	}
	return fmt.Sprintf("\nUpdate available: %s\nRun 'airvault upgrade' to update\n\n", result.LatestVersion)
}

func ShouldCheckUpdates(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "version", "upgrade", "--version", "-v", "--help", "-h", "help", "schema", "agent-context":
		return false
	}
	for _, arg := range args {
		if arg == "--format=json" || arg == "--json" {
			return false
		}
	}
	return true
}

func newUpdater() (*selfupdate.Updater, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, err
	}
	return selfupdate.NewUpdater(selfupdate.Config{Source: source, Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"}})
}

func checkWritePermission(exePath string) error {
	dir := filepath.Dir(exePath)
	tmp := filepath.Join(dir, ".airvault-update-test")
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("no write permission to %s; run with a user-owned install path or sudo airvault upgrade", dir)
	}
	f.Close()
	os.Remove(tmp)
	return nil
}

func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".airvault", cacheFile), nil
}

func loadCache() (*Cache, error) {
	path, err := cachePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cache Cache
	return &cache, json.Unmarshal(data, &cache)
}

func saveCache(cache Cache) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
