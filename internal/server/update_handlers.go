package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	appversion "xianyu-go/internal/version"
)

const (
	defaultGitHubReleaseAPI = "https://api.github.com/repos/HaoJunDeveloper/Ydisks-Xianyu-Helper/releases/latest"
	updateCommandEnv        = "XIANYU_UPDATE_COMMAND"
	updateAgentURL          = "XIANYU_UPDATE_AGENT_URL"
	updateAgentToken        = "XIANYU_UPDATE_AGENT_TOKEN"
)

type updateJobState struct {
	Running    bool   `json:"running"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Current    string `json:"current_version"`
	Latest     string `json:"latest_version"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

type updateRequest struct {
	Version string `json:"version"`
}

func (s *Server) mountUpdate(r chi.Router) {
	r.Get("/api/update/check", s.checkUpdate)
	r.Get("/api/update/releases", s.listReleases)
	r.Get("/api/update/status", s.updateStatus)
	r.Post("/api/update/apply", s.applyUpdate)
	r.Post("/api/update/rollback", s.rollbackUpdate)
}

func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) {
	release, err := fetchLatestRelease(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "检查 GitHub 最新版本失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  normalizeVersion(appversion.Version),
		"latest_version":   normalizeVersion(release.TagName),
		"update_available": newerVersion(release.TagName, appversion.Version),
		"release_url":      release.HTMLURL,
		"release_name":     release.Name,
		"release_notes":    release.Body,
	})
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateStateSnapshot())
}
func (s *Server) listReleases(w http.ResponseWriter, r *http.Request) {
	releases, err := fetchReleases(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "获取 GitHub 发布版本失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": releases})
}

func (s *Server) targetRelease(ctx context.Context, requested string) (githubRelease, error) {
	requested = normalizeVersion(requested)
	if requested == "" {
		return fetchLatestRelease(ctx)
	}
	releases, err := fetchReleases(ctx)
	if err != nil {
		return githubRelease{}, err
	}
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		if normalizeVersion(release.TagName) == requested {
			return release, nil
		}
	}
	return githubRelease{}, fmt.Errorf("未找到版本 %s", requested)
}

func fetchReleases(ctx context.Context) ([]githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/HaoJunDeveloper/Ydisks-Xianyu-Helper/releases?per_page=20", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ydisks-xianyu-helper-updater")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	filtered := releases[:0]
	for _, release := range releases {
		if release.Draft || release.Prerelease || !isValidVersion(release.TagName) {
			continue
		}
		filtered = append(filtered, release)
	}
	return filtered, nil
}

func (s *Server) applyUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	}
	target, err := s.targetRelease(r.Context(), req.Version)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "获取更新版本失败: "+err.Error())
		return
	}
	if !newerVersion(target.TagName, appversion.Version) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "当前已是最新版本"})
		return
	}
	s.startUpdate(w, target)
}

func (s *Server) rollbackUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	}
	if normalizeVersion(req.Version) == "" {
		writeErr(w, http.StatusBadRequest, "请选择要回退的版本")
		return
	}
	target, err := s.targetRelease(r.Context(), req.Version)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "获取回退版本失败: "+err.Error())
		return
	}
	if compareVersion(target.TagName, appversion.Version) < 0 {
		s.startUpdate(w, target)
		return
	}
	writeErr(w, http.StatusBadRequest, "回退版本必须低于当前版本")
}

func (s *Server) startUpdate(w http.ResponseWriter, release githubRelease) error {
	if strings.TrimSpace(os.Getenv(updateCommandEnv)) == "" && strings.TrimSpace(os.Getenv(updateAgentURL)) == "" {
		writeErr(w, http.StatusNotImplemented, "服务器未配置更新执行器，请配置 XIANYU_UPDATE_AGENT_URL 或 XIANYU_UPDATE_COMMAND")
		return nil
	}
	s.updateMu.Lock()
	if s.updateState.Running {
		state := s.updateState
		s.updateMu.Unlock()
		writeJSON(w, http.StatusAccepted, state)
		return nil
	}
	s.updateState = updateJobState{
		Running:   true,
		Status:    "starting",
		Message:   "更新任务已启动",
		Current:   normalizeVersion(appversion.Version),
		Latest:    normalizeVersion(release.TagName),
		StartedAt: time.Now().UnixMilli(),
	}
	s.updateMu.Unlock()
	go s.runUpdate(release)
	writeJSON(w, http.StatusAccepted, s.updateStateSnapshot())
	return nil
}

func (s *Server) runUpdate(release githubRelease) {
	err := runUpdateExecutor(release.TagName)
	s.updateStateMu(func(state *updateJobState) {
		state.Running = false
		state.FinishedAt = time.Now().UnixMilli()
		if err != nil {
			state.Status = "failed"
			state.Message = err.Error()
			return
		}
		state.Status = "completed"
		state.Message = "更新命令已执行，服务将重新启动"
	})
}

func runUpdateExecutor(tag string) error {
	if agentURL := strings.TrimSpace(os.Getenv(updateAgentURL)); agentURL != "" {
		body, _ := json.Marshal(map[string]string{"version": tag})
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, agentURL, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("创建更新代理请求失败: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token := strings.TrimSpace(os.Getenv(updateAgentToken)); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("调用更新代理失败: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return fmt.Errorf("更新代理返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
		}
		return nil
	}
	command := strings.TrimSpace(os.Getenv(updateCommandEnv))
	if command == "" {
		return errors.New("未配置更新执行器")
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "XIANYU_UPDATE_VERSION="+tag)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("更新命令失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defaultGitHubReleaseAPI, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ydisks-xianyu-helper-updater")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if release.Draft || release.Prerelease || !isValidVersion(release.TagName) {
		return githubRelease{}, errors.New("没有可用的正式 GitHub Release")
	}
	return release, nil
}

func (s *Server) updateStateSnapshot() updateJobState {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	return s.updateState
}

func (s *Server) updateStateMu(fn func(*updateJobState)) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	fn(&s.updateState)
}

func normalizeVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

func isValidVersion(value string) bool {
	parts := strings.Split(normalizeVersion(value), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func compareVersion(left, right string) int {
	if !isValidVersion(left) || !isValidVersion(right) {
		return 0
	}
	leftParts := versionParts(normalizeVersion(left))
	rightParts := versionParts(normalizeVersion(right))
	for i := range leftParts {
		if leftParts[i] < rightParts[i] {
			return -1
		}
		if leftParts[i] > rightParts[i] {
			return 1
		}
	}
	return 0
}

func newerVersion(latest, current string) bool {
	return compareVersion(latest, current) > 0
}

func versionParts(value string) [3]int {
	var parts [3]int
	for index, part := range strings.Split(normalizeVersion(value), ".") {
		parts[index], _ = strconv.Atoi(part)
	}
	return parts
}
