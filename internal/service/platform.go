package service

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/executor"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/runtimebundle"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/security"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/store"
)

var ErrAccountLocked = errors.New("账号已锁定")

const (
	maxClusterLogs   = 1000
	maxClusterAlerts = 1000
)

type session struct {
	Token       string
	User        string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	InitiatedAt time.Time
}

type PlatformService struct {
	store    store.Store
	sessionM sync.RWMutex
	sessions map[string]session
	writeMu  sync.Mutex
	executor *executor.Executor
	initOnce sync.Once
	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewPlatformService(dataStore store.Store) *PlatformService {
	svc := &PlatformService{
		store:    dataStore,
		sessions: map[string]session{},
		stopCh:   make(chan struct{}),
	}
	snapshot := dataStore.Snapshot()
	svc.sessionM.Lock()
	for _, s := range snapshot.Sessions {
		createdAt, err := time.Parse(time.RFC3339, s.CreatedAt)
		if err != nil {
			createdAt = time.Now().UTC()
		}
		expiresAt, err := time.Parse(time.RFC3339, s.ExpiresAt)
		if err != nil {
			expiresAt = createdAt.Add(24 * time.Hour)
		}
		initiatedAt, err := time.Parse(time.RFC3339, s.InitiatedAt)
		if err != nil {
			initiatedAt = createdAt
		}
		svc.sessions[s.Token] = session{Token: s.Token, User: s.UserID, CreatedAt: createdAt, ExpiresAt: expiresAt, InitiatedAt: initiatedAt}
	}
	svc.sessionM.Unlock()

	executorStore := &nodeStoreAdapter{svc: svc}
	pollSec := 10
	if v := os.Getenv("EXECUTOR_POLL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pollSec = n
		}
	}
	sshTimeoutSec := 8
	if v := os.Getenv("EXECUTOR_SSH_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			sshTimeoutSec = n
		}
	}
	svc.executor = executor.New(executor.Config{
		Store:           executorStore,
		PollIntervalSec: pollSec,
		SSHTimeoutSec:   sshTimeoutSec,
	})

	return svc
}

type nodeStoreAdapter struct {
	svc *PlatformService
}

func (a *nodeStoreAdapter) ListClusterNodes() []domain.ClusterNode {
	snap := a.svc.store.Snapshot()
	nodes := make([]domain.ClusterNode, len(snap.ClusterNodes))
	copy(nodes, snap.ClusterNodes)
	return nodes
}

func (a *nodeStoreAdapter) SaveClusterNodes(nodes []domain.ClusterNode) error {
	a.svc.writeMu.Lock()
	defer a.svc.writeMu.Unlock()
	snapshot := a.svc.store.Snapshot()
	snapshot.ClusterNodes = nodes
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "executor", "sync-cluster-nodes", "cluster", fmt.Sprintf("已同步 %d 个节点", len(nodes)))
	return a.svc.persistWithReport(snapshot)
}

func (a *nodeStoreAdapter) SaveClusterNodeStatus(node domain.ClusterNode) error {
	a.svc.writeMu.Lock()
	defer a.svc.writeMu.Unlock()
	snapshot := a.svc.store.Snapshot()
	for index := range snapshot.ClusterNodes {
		if snapshot.ClusterNodes[index].ID != node.ID {
			continue
		}
		current := snapshot.ClusterNodes[index]
		current.Status = node.Status
		current.Version = node.Version
		current.OS = node.OS
		current.Arch = node.Arch
		current.Kernel = node.Kernel
		current.ContainerRuntime = node.ContainerRuntime
		current.CapacityCPU = node.CapacityCPU
		current.CapacityMemory = node.CapacityMemory
		current.DiskCapacity = node.DiskCapacity
		current.CPUUsage = node.CPUUsage
		current.MemoryUsage = node.MemoryUsage
		current.DiskUsage = node.DiskUsage
		current.PodCount = node.PodCount
		current.LastHeartbeat = node.LastHeartbeat
		current.JoinStatus = node.JoinStatus
		current.LastJoinAttemptAt = node.LastJoinAttemptAt
		current.LastJoinMessage = node.LastJoinMessage
		current.ProvisionStatus = node.ProvisionStatus
		current.ProvisionTaskID = node.ProvisionTaskID
		current.SGXStatus = node.SGXStatus
		current.RuntimeStatus = node.RuntimeStatus
		current.RxBytes = node.RxBytes
		current.TxBytes = node.TxBytes
		current.RxRate = node.RxRate
		current.TxRate = node.TxRate
		snapshot.ClusterNodes[index] = current
		return a.svc.persistWithReport(snapshot)
	}
	return store.ErrNotFound
}

func (a *nodeStoreAdapter) ListInstallPackages() []domain.InstallPackage {
	snap := a.svc.store.Snapshot()
	packages := make([]domain.InstallPackage, len(snap.InstallPackages))
	copy(packages, snap.InstallPackages)
	return packages
}

func (a *nodeStoreAdapter) ListProvisioningTasks() []domain.ProvisioningTask {
	snap := a.svc.store.Snapshot()
	tasks := make([]domain.ProvisioningTask, len(snap.ProvisioningTasks))
	copy(tasks, snap.ProvisioningTasks)
	return tasks
}

func (a *nodeStoreAdapter) SaveProvisioningTaskStatus(task domain.ProvisioningTask) error {
	return a.svc.SaveProvisioningTaskStatus(task)
}

func (s *PlatformService) Snapshot() store.Snapshot {
	snapshot := s.store.Snapshot()
	s.initOnce.Do(func() {
		if len(snapshot.Reports) == 0 {
			s.writeMu.Lock()
			defer s.writeMu.Unlock()
			current := s.store.Snapshot()
			if len(current.Reports) == 0 {
				report := s.buildComplianceReport(current)
				current.Reports = []domain.ComplianceReport{report}
				if err := s.store.Replace(current); err != nil {
					log.Printf("初始化合规报告持久化失败: %v", err)
				}
			}
		}
	})
	snapshot = s.store.Snapshot()
	snapshot = s.normalizeSnapshotCollections(snapshot)
	return snapshot
}

func (s *PlatformService) BuildDashboardSummary() domain.DashboardSummary {
	snap := s.Snapshot()

	totalNodes := len(snap.ClusterNodes)
	var readyNodes int
	var cpuSum, memSum, diskSum int
	var throughputSum float64
	var sgxReady int
	var allNodes []domain.DashboardNodeInfo

	for _, n := range snap.ClusterNodes {
		if n.Status == "ready" {
			readyNodes++
		}
		if n.SGXStatus == domain.SGXReady {
			sgxReady++
		}
		cpuSum += n.CPUUsage
		memSum += n.MemoryUsage
		diskSum += n.DiskUsage
		throughputSum += n.RxRate + n.TxRate
		allNodes = append(allNodes, domain.DashboardNodeInfo{
			ID:        n.ID,
			Name:      n.Name,
			Role:      n.Role,
			Status:    n.Status,
			CPUUsage:  n.CPUUsage,
			MemUsage:  n.MemoryUsage,
			DiskUsage: n.DiskUsage,
			SGXStatus: string(n.SGXStatus),
		})
	}

	var avgCPU, avgMem, avgDisk int
	if totalNodes > 0 {
		avgCPU = cpuSum / totalNodes
		avgMem = memSum / totalNodes
		avgDisk = diskSum / totalNodes
	}

	totalComponents := len(snap.Components)
	var healthyComponents int
	for _, c := range snap.Components {
		if c.Status == domain.ComponentDeployed {
			healthyComponents++
		}
	}

	totalImages := len(snap.Images)
	var signedImages int
	for _, img := range snap.Images {
		if img.Signed {
			signedImages++
		}
	}

	var activeAlerts int
	for _, a := range snap.ClusterAlerts {
		if a.Status == "open" {
			activeAlerts++
		}
	}

	var complianceScore int
	for _, r := range snap.Reports {
		if r.Score > complianceScore {
			complianceScore = r.Score
		}
	}

	return domain.DashboardSummary{
		TotalNodes:        totalNodes,
		ReadyNodes:        readyNodes,
		TotalComponents:   totalComponents,
		HealthyComponents: healthyComponents,
		TotalImages:       totalImages,
		SignedImages:      signedImages,
		ActiveAlerts:      activeAlerts,
		ComplianceScore:   complianceScore,
		ClusterCPUUsage:   avgCPU,
		ClusterMemUsage:   avgMem,
		ClusterDiskUsage:  avgDisk,
		K3sVersion:        snap.ClusterUpgrade.CurrentVersion,
		NetworkThroughput: throughputSum,
		SGXReadyNodes:     sgxReady,
		Nodes:             allNodes,
	}
}

func (s *PlatformService) normalizeSnapshotCollections(snapshot store.Snapshot) store.Snapshot {
	snapshot = s.reconcileClusterUpgrade(snapshot)
	for index := range snapshot.Users {
		snapshot.Users[index].PasswordHash = ""
	}
	for index := range snapshot.ClusterNodes {
		snapshot.ClusterNodes[index].SSHPassword = ""
		snapshot.ClusterNodes[index].SSHPasswordCiphertext = ""
		snapshot.ClusterNodes[index].SSHPasswordConfigured = snapshot.ClusterNodes[index].SSHPasswordConfigured || (snapshot.ClusterNodes[index].SSHHost != "" && snapshot.ClusterNodes[index].SSHUsername != "" && !snapshot.ClusterNodes[index].SSHPasswordConfigured)
	}
	snapshot.LoginFailures = nil
	snapshot.LoginLockedUntil = nil
	snapshot.Sessions = nil
	snapshot = ensureSnapshotCollections(snapshot)
	if s.builtInBundleHint() {
		hasHint := false
		for _, h := range snapshot.ManifestHints {
			if h == "built-in-bundle-available" {
				hasHint = true
				break
			}
		}
		if !hasHint {
			snapshot.ManifestHints = append(snapshot.ManifestHints, "built-in-bundle-available")
		}
	}
	return snapshot
}

func ensureSnapshotCollections(snapshot store.Snapshot) store.Snapshot {
	if snapshot.Images == nil {
		snapshot.Images = []domain.ImageAsset{}
	}
	if snapshot.Components == nil {
		snapshot.Components = []domain.ComponentDefinition{}
	}
	if snapshot.Enclaves == nil {
		snapshot.Enclaves = []domain.EnclaveProfile{}
	}
	if snapshot.Networks == nil {
		snapshot.Networks = []domain.NetworkAttachment{}
	}
	if snapshot.Attestations == nil {
		snapshot.Attestations = []domain.AttestationRecord{}
	}
	if snapshot.Reports == nil {
		snapshot.Reports = []domain.ComplianceReport{}
	}
	if snapshot.Users == nil {
		snapshot.Users = []domain.User{}
	}
	if snapshot.ManifestHints == nil {
		snapshot.ManifestHints = []string{}
	}
	if snapshot.ClusterNodes == nil {
		snapshot.ClusterNodes = []domain.ClusterNode{}
	}
	if snapshot.ClusterQuotas == nil {
		snapshot.ClusterQuotas = []domain.ClusterQuota{}
	}
	if snapshot.ClusterAlerts == nil {
		snapshot.ClusterAlerts = []domain.ClusterAlert{}
	}
	if snapshot.ClusterLogs == nil {
		snapshot.ClusterLogs = []domain.ClusterLog{}
	}
	if snapshot.ProvisioningTasks == nil {
		snapshot.ProvisioningTasks = []domain.ProvisioningTask{}
	}
	if snapshot.InstallPackages == nil {
		snapshot.InstallPackages = []domain.InstallPackage{}
	}
	if snapshot.MarketplaceApps == nil {
		snapshot.MarketplaceApps = []domain.MarketplaceApp{}
	}
	if snapshot.CatalogItems == nil {
		snapshot.CatalogItems = []domain.ComponentCatalogItem{}
	}
	if snapshot.IsolationPolicies == nil {
		snapshot.IsolationPolicies = []domain.IsolationPolicy{}
	}
	if snapshot.EnclaveResources == nil {
		snapshot.EnclaveResources = []domain.EnclaveResource{}
	}
	if snapshot.EnclaveKeys == nil {
		snapshot.EnclaveKeys = []domain.EnclaveKeyMaterial{}
	}
	if snapshot.EnclaveInspections == nil {
		snapshot.EnclaveInspections = []domain.EnclaveInspection{}
	}
	if snapshot.SecurityPolicies == nil {
		snapshot.SecurityPolicies = []domain.SecurityPolicyRule{}
	}
	if snapshot.ComplianceTasks == nil {
		snapshot.ComplianceTasks = []domain.ComplianceTask{}
	}
	if snapshot.SystemSettings == nil {
		snapshot.SystemSettings = []domain.SystemSetting{}
	}
	if snapshot.AuditEvents == nil {
		snapshot.AuditEvents = []domain.AuditEvent{}
	}
	if snapshot.TopoLinks == nil {
		snapshot.TopoLinks = []domain.TopologyLink{}
	}
	if snapshot.TopoEgress == nil {
		snapshot.TopoEgress = []domain.TopologyNode{}
	}
	if snapshot.Plugins == nil {
		snapshot.Plugins = []domain.PluginDefinition{}
	}
	return snapshot
}

func (s *PlatformService) reconcileClusterUpgrade(snapshot store.Snapshot) store.Snapshot {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	status := snapshot.ClusterUpgrade
	if status.Status != "running" && status.Status != "pending_executor" {
		return snapshot
	}
	if status.Status == "running" && status.Progress < 5 {
		snapshot.ClusterUpgrade.Progress = 5
		if snapshot.ClusterUpgrade.Message == "" {
			snapshot.ClusterUpgrade.Message = "升级命令已下发，等待真实执行器回写结果"
		}
		if err := s.store.Replace(snapshot); err != nil {
			log.Printf("升级状态持久化失败: %v", err)
		}
	}
	return snapshot
}

func (s *PlatformService) builtInBundleHint() bool {
	return validateBuiltInBundleAvailable() == nil
}

func (s *PlatformService) Health() error {
	return s.store.Ping()
}

func (s *PlatformService) Login(payload domain.LoginRequest) (domain.LoginResponse, error) {
	username := strings.TrimSpace(payload.Username)
	if username == "" || strings.TrimSpace(payload.Password) == "" {
		return domain.LoginResponse{}, errors.New("用户名和密码不能为空")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()

	if lockStr, locked := snapshot.LoginLockedUntil[username]; locked {
		lockTime, parseErr := time.Parse(time.RFC3339, lockStr)
		if parseErr == nil && time.Now().UTC().Before(lockTime) {
			remaining := lockTime.Sub(time.Now().UTC()).Round(time.Second)
			return domain.LoginResponse{}, fmt.Errorf("%w 请在 %v 后重试", ErrAccountLocked, remaining)
		}
	}

	var matchedIdx int = -1
	for i, user := range snapshot.Users {
		if user.Username == username && security.VerifyPassword(user.PasswordHash, payload.Password) {
			if user.Status != domain.UserActive {
				matchedIdx = -1
				break
			}
			matchedIdx = i
			break
		}
	}

	if matchedIdx < 0 {
		_ = security.VerifyPassword("$2a$12$dummyDummyDummyDummyDummyDummyDummyDummyDummyDummy", payload.Password)
		if snapshot.LoginFailures == nil {
			snapshot.LoginFailures = map[string]int{}
		}
		if snapshot.LoginLockedUntil == nil {
			snapshot.LoginLockedUntil = map[string]string{}
		}
		snapshot.LoginFailures[username] = snapshot.LoginFailures[username] + 1
		if snapshot.LoginFailures[username] >= 5 {
			snapshot.LoginLockedUntil[username] = time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339)
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, username, "login-locked", username, fmt.Sprintf("连续%d次失败", snapshot.LoginFailures[username]))
		} else {
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, username, "login-failed", username, fmt.Sprintf("第%d次失败", snapshot.LoginFailures[username]))
		}
		if err := s.store.Replace(snapshot); err != nil {
			log.Printf("[security] ERROR: 登录失败计数器持久化失败: %v", err)
			return domain.LoginResponse{}, fmt.Errorf("系统暂时不可用，请稍后重试: %w", err)
		}
		return domain.LoginResponse{}, errors.New("用户名或密码错误")
	}

	authenticatedUser := snapshot.Users[matchedIdx]
	delete(snapshot.LoginLockedUntil, username)
	delete(snapshot.LoginFailures, username)
	snapshot.Users[matchedIdx].LastLoginAt = time.Now().UTC().Format(time.RFC3339)

	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, username, "login", authenticatedUser.ID, "success")
	sessionTTL := s.getSessionTimeout()
	now := time.Now().UTC()
	token := security.GenerateToken(authenticatedUser.ID)
	s.sessionM.Lock()
	s.sessions[token] = session{Token: token, User: authenticatedUser.ID, CreatedAt: now, ExpiresAt: now.Add(sessionTTL), InitiatedAt: now}
	s.sessionM.Unlock()
	s.applySessionsToSnapshot(&snapshot)
	if err := s.store.Replace(snapshot); err != nil {
		return domain.LoginResponse{}, err
	}
	return domain.LoginResponse{Token: token, User: toUserView(authenticatedUser)}, nil
}

func (s *PlatformService) Logout(token string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.sessionM.Lock()
	sessionEntry, ok := s.sessions[token]
	delete(s.sessions, token)
	s.sessionM.Unlock()
	if !ok {
		return
	}
	snapshot := s.store.Snapshot()
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, sessionEntry.User, "logout", sessionEntry.User, "success")
	s.applySessionsToSnapshot(&snapshot)
	if err := s.persistWithReport(snapshot); err != nil {
		log.Printf("Logout: persistWithReport failed: %v", err)
	}
}

func (s *PlatformService) CurrentUser(token string) (domain.UserView, error) {
	tokenUserID, tokenValid := security.VerifyToken(token)
	if !tokenValid {
		return domain.UserView{}, errors.New("登录状态已失效")
	}
	s.sessionM.RLock()
	sessionEntry, ok := s.sessions[token]
	s.sessionM.RUnlock()
	if !ok {
		return domain.UserView{}, errors.New("登录状态已失效")
	}
	if sessionEntry.User != tokenUserID {
		return domain.UserView{}, errors.New("登录状态已失效")
	}
	now := time.Now().UTC()
	maxSessionLifetime := 7 * 24 * time.Hour
	if !sessionEntry.InitiatedAt.IsZero() && now.After(sessionEntry.InitiatedAt.Add(maxSessionLifetime)) {
		s.writeMu.Lock()
		s.sessionM.Lock()
		delete(s.sessions, token)
		s.sessionM.Unlock()
		s.writeSessionsToSnapshot()
		s.writeMu.Unlock()
		return domain.UserView{}, errors.New("登录状态已失效，请重新登录")
	}
	if now.After(sessionEntry.ExpiresAt) {
		s.writeMu.Lock()
		s.sessionM.Lock()
		delete(s.sessions, token)
		s.sessionM.Unlock()
		s.writeSessionsToSnapshot()
		s.writeMu.Unlock()
		return domain.UserView{}, errors.New("登录状态已失效")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for _, user := range snapshot.Users {
		if user.ID == sessionEntry.User {
			if user.Status != domain.UserActive {
				s.sessionM.Lock()
				delete(s.sessions, token)
				s.sessionM.Unlock()
				s.writeSessionsToSnapshot()
				return domain.UserView{}, errors.New("用户已被禁用")
			}
			sessionTTL := s.getSessionTimeout()
			s.sessionM.Lock()
			sessionEntry.ExpiresAt = now.Add(sessionTTL)
			s.sessions[token] = sessionEntry
			s.sessionM.Unlock()
			s.applySessionsToSnapshot(&snapshot)
			if err := s.store.Replace(snapshot); err != nil {
				return domain.UserView{}, err
			}
			return toUserView(user), nil
		}
	}
	return domain.UserView{}, store.ErrNotFound
}

func (s *PlatformService) getSessionTimeout() time.Duration {
	snapshot := s.store.Snapshot()
	for _, setting := range snapshot.SystemSettings {
		if setting.ID == "setting-session-timeout" {
			d, parseErr := time.ParseDuration(setting.Value)
			if parseErr == nil && d >= time.Minute {
				return d
			}
			break
		}
	}
	return 30 * time.Minute
}

func (s *PlatformService) cleanExpiredSessions() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := time.Now().UTC()
	changed := false
	func() {
		s.sessionM.Lock()
		defer s.sessionM.Unlock()
		for token, entry := range s.sessions {
			if now.After(entry.ExpiresAt) {
				delete(s.sessions, token)
				changed = true
			}
		}
	}()
	snapshot := s.store.Snapshot()
	cleanupLoginFailures := false
	if snapshot.LoginLockedUntil != nil {
		for username, lockedUntil := range snapshot.LoginLockedUntil {
			lockedTime, err := time.Parse(time.RFC3339, lockedUntil)
			if err != nil || now.After(lockedTime) {
				delete(snapshot.LoginLockedUntil, username)
				delete(snapshot.LoginFailures, username)
				cleanupLoginFailures = true
			}
		}
	}
	if changed {
		s.applySessionsToSnapshot(&snapshot)
	}
	if changed || cleanupLoginFailures {
		if err := s.store.Replace(snapshot); err != nil {
			log.Printf("cleanExpiredSessions: Replace failed: %v", err)
		}
	}
}

func (s *PlatformService) StartSessionCleanup() {
	s.executor.Start(context.Background())
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[session-cleanup] panic recovered: %v", r)
			}
		}()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		auditTicker := time.NewTicker(1 * time.Hour)
		defer auditTicker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.cleanExpiredSessions()
				_ = s.store.Checkpoint()
			case <-auditTicker.C:
				s.store.CleanupAudit()
			}
		}
	}()
}

func (s *PlatformService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.executor.Stop()
}

func (s *PlatformService) ChangePassword(userID string, payload domain.ChangePasswordPayload) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(payload.CurrentPassword) == "" || strings.TrimSpace(payload.NewPassword) == "" {
		return errors.New("当前密码和新密码不能为空")
	}
	if strings.TrimSpace(payload.NewPassword) == payload.CurrentPassword {
		return errors.New("新密码需要与当前密码不同")
	}
	if len(payload.NewPassword) < 8 {
		return errors.New("新密码长度至少为 8 位")
	}
	hasLetter := false
	hasDigit := false
	for _, c := range payload.NewPassword {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			hasLetter = true
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("新密码须至少包含一个字母和一个数字")
	}
	snapshot := s.store.Snapshot()
	for index, user := range snapshot.Users {
		if user.ID != userID {
			continue
		}
		if !security.VerifyPassword(user.PasswordHash, payload.CurrentPassword) {
			return errors.New("当前密码错误")
		}
		newHash, hashErr := security.HashPassword(payload.NewPassword)
		if hashErr != nil {
			return fmt.Errorf("密码加密失败: %w", hashErr)
		}
		snapshot.Users[index].PasswordHash = newHash
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, user.Username, "change-password", user.ID, "success")
		s.revokeUserSessions(userID)
		s.applySessionsToSnapshot(&snapshot)
		return s.persistWithReport(snapshot)
	}
	return store.ErrNotFound
}

func (s *PlatformService) revokeUserSessions(userID string) {
	s.sessionM.Lock()
	defer s.sessionM.Unlock()
	for token, entry := range s.sessions {
		if entry.User == userID {
			delete(s.sessions, token)
		}
	}
}

func (s *PlatformService) persistSessions() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writeSessionsToSnapshot()
}

func (s *PlatformService) writeSessionsToSnapshot() {
	snapshot := s.store.Snapshot()
	s.applySessionsToSnapshot(&snapshot)
	if err := s.store.Replace(snapshot); err != nil {
		log.Printf("writeSessionsToSnapshot: Replace failed: %v", err)
	}
}

func (s *PlatformService) applySessionsToSnapshot(snapshot *store.Snapshot) {
	s.sessionM.RLock()
	defer s.sessionM.RUnlock()
	sessionList := make([]domain.Session, 0, len(s.sessions))
	for _, entry := range s.sessions {
		sessionList = append(sessionList, domain.Session{
			Token:       entry.Token,
			UserID:      entry.User,
			CreatedAt:   entry.CreatedAt.Format(time.RFC3339),
			ExpiresAt:   entry.ExpiresAt.Format(time.RFC3339),
			InitiatedAt: entry.InitiatedAt.Format(time.RFC3339),
		})
	}
	snapshot.Sessions = sessionList
}

func (s *PlatformService) RecordAudit(actor string, action string, target string, result string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, action, target, result)
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) FindUser(id string) (domain.UserView, error) {
	for _, user := range s.store.Snapshot().Users {
		if user.ID == id {
			return toUserView(user), nil
		}
	}
	return domain.UserView{}, store.ErrNotFound
}

func (s *PlatformService) ListUsers() []domain.UserView {
	snapshot := s.store.Snapshot()
	result := make([]domain.UserView, 0, len(snapshot.Users))
	for _, user := range snapshot.Users {
		result = append(result, toUserView(user))
	}
	return result
}

func (s *PlatformService) SaveUser(payload domain.UserPayload, actor string) (domain.UserView, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(payload.ID) == "" {
		payload.ID = uniqueEntityID("user", payload.Username)
	}
	if payload.Role != domain.RolePlatformAdmin && payload.Role != domain.RoleSecurityAdmin && payload.Role != domain.RoleAuditor && payload.Role != domain.RoleOperator {
		return domain.UserView{}, errors.New("用户角色无效")
	}
	if payload.Status != domain.UserActive && payload.Status != domain.UserDisabled {
		return domain.UserView{}, errors.New("用户状态无效")
	}
	snapshot := s.store.Snapshot()
	for index, user := range snapshot.Users {
		if user.ID == payload.ID {
			if strings.TrimSpace(payload.Username) == "" {
				payload.Username = user.Username
			}
			if strings.TrimSpace(payload.DisplayName) == "" {
				payload.DisplayName = user.DisplayName
			}
			if strings.EqualFold(user.Username, actor) && (payload.Role != user.Role || payload.Status != user.Status) {
				return domain.UserView{}, errors.New("不能修改当前登录账号的角色或状态")
			}
			for _, other := range snapshot.Users {
				if other.ID != payload.ID && other.Username == payload.Username {
					return domain.UserView{}, errors.New("用户名已存在")
				}
			}
			if strings.TrimSpace(payload.Password) != "" {
				newHash, hashErr := security.HashPassword(payload.Password)
				if hashErr != nil {
					return domain.UserView{}, fmt.Errorf("密码加密失败: %w", hashErr)
				}
				user.PasswordHash = newHash
			}
			user.Username = payload.Username
			user.DisplayName = payload.DisplayName
			user.Role = payload.Role
			user.Status = payload.Status
			snapshot.Users[index] = user
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "update-user", payload.ID, "success")
			if err := s.persistWithReport(snapshot); err != nil {
				return domain.UserView{}, err
			}
			return toUserView(user), nil
		}
	}
	if strings.TrimSpace(payload.Username) == "" || strings.TrimSpace(payload.DisplayName) == "" {
		return domain.UserView{}, errors.New("新建用户需要用户名和显示名称")
	}
	if strings.TrimSpace(payload.Password) == "" {
		return domain.UserView{}, errors.New("新建用户需要初始密码")
	}
	for _, existing := range snapshot.Users {
		if existing.ID == payload.ID {
			return domain.UserView{}, errors.New("用户 ID 已存在")
		}
		if existing.Username == payload.Username {
			return domain.UserView{}, errors.New("用户名已存在")
		}
	}
	newHash, hashErr := security.HashPassword(payload.Password)
	if hashErr != nil {
		return domain.UserView{}, fmt.Errorf("密码加密失败: %w", hashErr)
	}
	user := domain.User{
		ID:           payload.ID,
		Username:     payload.Username,
		DisplayName:  payload.DisplayName,
		Role:         payload.Role,
		Status:       payload.Status,
		PasswordHash: newHash,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	snapshot.Users = append(snapshot.Users, user)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "create-user", payload.Username, "success")
	if err := s.persistWithReport(snapshot); err != nil {
		return domain.UserView{}, err
	}
	return toUserView(user), nil
}

func (s *PlatformService) DeleteUser(id string, actor string) error {
	if strings.EqualFold(id, actor) {
		return fmt.Errorf("%w: 不能删除当前登录账号", store.ErrConflict)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	var deletedUser *domain.User
	platformAdminCount := 0
	for i := range snapshot.Users {
		if snapshot.Users[i].Role == domain.RolePlatformAdmin {
			platformAdminCount++
		}
		if snapshot.Users[i].ID == id {
			deletedUser = &snapshot.Users[i]
		}
	}
	if deletedUser == nil {
		return store.ErrNotFound
	}
	if deletedUser.Role == domain.RolePlatformAdmin && platformAdminCount <= 1 {
		return fmt.Errorf("%w: 不能删除最后一位平台管理员", store.ErrConflict)
	}
	updated := make([]domain.User, 0, len(snapshot.Users))
	for _, user := range snapshot.Users {
		if user.ID == id {
			continue
		}
		updated = append(updated, user)
	}
	snapshot.Users = updated
	s.revokeUserSessions(id)
	s.applySessionsToSnapshot(&snapshot)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "delete-user", deletedUser.Username, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveImage(image domain.ImageAsset) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(image.ID) == "" {
		image.ID = uniqueEntityID("image", image.Name)
	}
	if strings.TrimSpace(image.Name) == "" {
		return errors.New("镜像名称不能为空")
	}
	if strings.TrimSpace(image.Registry) == "" || strings.TrimSpace(image.Repository) == "" || strings.TrimSpace(image.Tag) == "" {
		return errors.New("镜像 Registry、Repository 和 Tag 不能为空")
	}
	if len(image.Registry) > 255 || len(image.Repository) > 255 || len(image.Tag) > 128 {
		return errors.New("镜像 Registry/Repository/Tag 长度超过限制")
	}
	if strings.ContainsAny(image.Repository, " \t\r\n") || strings.ContainsAny(image.Registry, " \t\r\n") || strings.ContainsAny(image.Tag, " \t\r\n") {
		return errors.New("镜像字段包含非法空白字符")
	}
	if !validOCIReference(image.Repository) {
		return errors.New("镜像 Repository 格式不符合 OCI 规范（仅允许小写字母、数字、._-/）")
	}
	if !validOCITag(image.Tag) {
		return errors.New("镜像 Tag 格式不符合 OCI 规范（仅允许字母、数字、._-）")
	}
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	execOK, execMsg := s.executor.ExecuteImageValidation(image)
	if image.LastScanAt == "" {
		image.LastScanAt = now
	}
	if execOK && strings.TrimSpace(image.Vulnerability) == "" {
		image.Vulnerability = "low"
	}
	if !execOK && strings.TrimSpace(image.Vulnerability) == "" {
		image.Vulnerability = "pending_executor"
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-image-pending-" + image.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: image.Name, Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-image-pending-" + image.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "image", Level: "warning", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
	} else if execOK {
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-image-validated-" + image.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "image", Level: "info", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
	}
	for index, item := range snapshot.Images {
		if item.ID == image.ID {
			snapshot.Images[index] = image
			result := "success"
			if !execOK {
				result = "pending-executor"
			}
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-image", image.Name, result)
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.Images = append(snapshot.Images, image)
	result := "success"
	if !execOK {
		result = "pending-executor"
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-image", image.Name, result)
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) BuildImage(request domain.ImageBuildRequest) (domain.ImageBuildResult, error) {
	name := strings.TrimSpace(request.Name)
	registry := strings.TrimSpace(request.Registry)
	repository := strings.TrimSpace(request.Repository)
	tag := strings.TrimSpace(request.Tag)
	sourcePackage := strings.TrimSpace(request.SourcePackage)
	dockerfilePath := strings.TrimSpace(request.DockerfilePath)
	buildArgs := strings.TrimSpace(request.BuildArgs)
	if name == "" {
		return domain.ImageBuildResult{}, errors.New("镜像名称不能为空")
	}
	if registry == "" || repository == "" || tag == "" {
		return domain.ImageBuildResult{}, errors.New("镜像 Registry、Repository 和 Tag 不能为空")
	}
	if sourcePackage == "" {
		return domain.ImageBuildResult{}, errors.New("安全组件源码或制品包不能为空")
	}
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	if strings.ContainsAny(sourcePackage+dockerfilePath+buildArgs, "\x00") {
		return domain.ImageBuildResult{}, errors.New("构建参数包含非法字符")
	}
	if strings.Contains(sourcePackage, "../") || strings.Contains(dockerfilePath, "../") {
		return domain.ImageBuildResult{}, errors.New("构建路径包含非法目录遍历")
	}
	if len(buildArgs) > 1000 {
		return domain.ImageBuildResult{}, errors.New("构建参数长度超过限制")
	}
	image := domain.ImageAsset{
		ID:            uniqueEntityID("image", name+"-"+tag),
		Name:          name,
		Registry:      registry,
		Repository:    repository,
		Tag:           tag,
		Signed:        request.EnableSignature,
		SBOM:          request.GenerateSBOM,
		EnclaveReady:  request.EnableSGXRuntime,
		Vulnerability: "scanning",
	}
	if err := s.SaveImage(image); err != nil {
		return domain.ImageBuildResult{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	logText := fmt.Sprintf("镜像构建任务已登记: source=%s dockerfile=%s target=%s/%s:%s signature=%t sbom=%t sgx=%t", sourcePackage, dockerfilePath, registry, repository, tag, request.EnableSignature, request.GenerateSBOM, request.EnableSGXRuntime)
	for _, item := range snapshot.Images {
		if item.ID == image.ID {
			image = item
			break
		}
	}
	snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-image-build-" + image.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "image", Level: "info", Message: logText, RecordedAt: now}}, snapshot.ClusterLogs...)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "build-image", image.Name, "scanning")
	if err := s.persistWithReport(snapshot); err != nil {
		return domain.ImageBuildResult{}, err
	}

	go s.runImageBuild(image, request)

	return domain.ImageBuildResult{Image: image, Log: logText}, nil
}

func (s *PlatformService) runImageBuild(image domain.ImageAsset, request domain.ImageBuildRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s._runImageBuild(ctx, image, request)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("[image-build] timeout for %s: %v", image.ID, ctx.Err())
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		snapshot := s.store.Snapshot()
		for i := range snapshot.Images {
			if snapshot.Images[i].ID == image.ID {
				snapshot.Images[i].LastScanAt = time.Now().UTC().Format(time.RFC3339)
				snapshot.Images[i].Vulnerability = "high"
				snapshot.ClusterLogs = append([]domain.ClusterLog{{
					ID:         fmt.Sprintf("log-build-timeout-%s-%d", image.ID, time.Now().UTC().UnixNano()),
					NodeID:     "cluster",
					Category:   "image",
					Level:      "error",
					Message:    fmt.Sprintf("镜像构建超时: %v", ctx.Err()),
					RecordedAt: time.Now().UTC().Format(time.RFC3339),
				}}, snapshot.ClusterLogs...)
				snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "build-image-result", snapshot.Images[i].Name, "timeout")
			if err := s.store.Replace(trimSnapshotCollections(snapshot)); err != nil {
				log.Printf("[image-build] timeout persist for %s failed: %v", image.ID, err)
			}
				break
			}
		}
	}
}

func (s *PlatformService) _runImageBuild(ctx context.Context, image domain.ImageAsset, request domain.ImageBuildRequest) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[image-build] panic recovered: %v", r)
			s.writeMu.Lock()
			defer s.writeMu.Unlock()
			now := time.Now().UTC().Format(time.RFC3339)
			snapshot := s.store.Snapshot()
			for i := range snapshot.Images {
				if snapshot.Images[i].ID == image.ID {
					snapshot.Images[i].LastScanAt = now
					snapshot.Images[i].Vulnerability = "critical"
					snapshot.ClusterLogs = append([]domain.ClusterLog{{
						ID:         fmt.Sprintf("log-build-panic-%s-%d", image.ID, time.Now().UTC().UnixNano()),
						NodeID:     "cluster",
						Category:   "image",
						Level:      "error",
						Message:    fmt.Sprintf("镜像构建内部错误: %v", r),
						RecordedAt: now,
					}}, snapshot.ClusterLogs...)
					snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "build-image-result", snapshot.Images[i].Name, "critical-error")
					if err := s.store.Replace(trimSnapshotCollections(snapshot)); err != nil {
						log.Printf("[image-build] panic persist for %s failed: %v", image.ID, err)
					}
					break
				}
			}
		}
	}()
	execOK, execMsg := s.executor.ExecuteImageBuild(request)
	imageID := image.ID
	now := time.Now().UTC().Format(time.RFC3339)

	if ctx.Err() != nil {
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for i := range snapshot.Images {
		if snapshot.Images[i].ID == imageID {
			snapshot.Images[i].LastScanAt = now
			if execOK {
				snapshot.Images[i].Vulnerability = "low"
				if idx := strings.Index(execMsg, "DIGEST:"); idx >= 0 {
					snapshot.Images[i].Digest = strings.TrimSpace(execMsg[idx+7:])
				}
			} else {
				snapshot.Images[i].Vulnerability = "high"
			}
			level := "info"
			if !execOK {
				level = "error"
			}
			snapshot.ClusterLogs = append([]domain.ClusterLog{{
				ID:         fmt.Sprintf("log-build-result-%s-%d", imageID, time.Now().UTC().UnixNano()),
				NodeID:     "cluster",
				Category:   "image",
				Level:      level,
				Message:    execMsg,
				RecordedAt: now,
			}}, snapshot.ClusterLogs...)
			resultLabel := "success"
			if !execOK {
				resultLabel = "failure"
			}
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "build-image-result", snapshot.Images[i].Name, resultLabel)
			if err := s.store.Replace(trimSnapshotCollections(snapshot)); err != nil {
				log.Printf("[image-build] persist result for %s failed: %v", imageID, err)
			}
			break
		}
	}
}

func (s *PlatformService) DeleteImage(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for _, component := range snapshot.Components {
		if component.Image == id {
			return fmt.Errorf("%w: 存在关联组件，镜像暂时无法删除", store.ErrConflict)
		}
	}
	updated := make([]domain.ImageAsset, 0, len(snapshot.Images))
	found := false
	for _, item := range snapshot.Images {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.Images = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-image", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ListComponents() []domain.ComponentDefinition {
	return s.store.Snapshot().Components
}

func (s *PlatformService) SaveComponent(component domain.ComponentDefinition) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(component.ID) == "" {
		component.ID = uniqueEntityID("component", component.Name)
	}
	if strings.TrimSpace(component.Name) == "" {
		return errors.New("组件名称不能为空")
	}
	if component.Replicas < 1 {
		return errors.New("组件副本数必须大于 0")
	}
	snapshot := s.store.Snapshot()
	if !s.imageExists(snapshot, component.Image) {
		return errors.New("组件引用的镜像不存在")
	}
	for _, networkID := range component.NetworkAttachments {
		if !s.networkExists(snapshot, networkID) {
			return fmt.Errorf("网络 %s 不存在", networkID)
		}
	}
	if component.Isolation != domain.IsolationStandard && component.Isolation != domain.IsolationEnclave {
		return errors.New("组件隔离模式必须为 standard 或 enclave")
	}
	if strings.ContainsAny(component.ID, "\r\n") || strings.ContainsAny(component.Name, "\r\n") || strings.ContainsAny(component.Namespace, "\r\n") {
		return errors.New("组件字段包含非法换行字符")
	}
	if len(component.Namespace) > 63 || len(component.ID) > 253 {
		return errors.New("组件 ID 或 Namespace 长度超过 Kubernetes 限制")
	}
	if component.Status == "" {
		component.Status = domain.ComponentDraft
	}
	for index, item := range snapshot.Components {
		if item.ID == component.ID {
			snapshot.Components[index] = component
			s.attachComponentRelations(&snapshot, component)
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-component", component.Name, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.Components = append(snapshot.Components, component)
	s.attachComponentRelations(&snapshot, component)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-component", component.Name, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveInstallPackage(pkg domain.InstallPackage) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(pkg.ID) == "" {
		pkg.ID = uniqueEntityID("package", pkg.Name)
	}
	if strings.TrimSpace(pkg.Name) == "" || strings.TrimSpace(pkg.Version) == "" {
		return errors.New("安装包名称和版本不能为空")
	}
	if pkg.Mode != domain.InstallModeISO && pkg.Mode != domain.InstallModePackage && pkg.Mode != domain.InstallModeOffline {
		return errors.New("安装包模式无效")
	}
	if strings.TrimSpace(pkg.FilePath) == "" {
		return errors.New("安装包文件路径不能为空")
	}
	if strings.Contains(pkg.FilePath, "../") || strings.Contains(pkg.FilePath, "..\\") {
		return errors.New("安装包文件路径包含非法字符")
	}
	cleanPath := filepath.Clean(pkg.FilePath)
	if strings.HasPrefix(cleanPath, "..") {
		return errors.New("安装包文件路径无效")
	}
	fileInfo, statErr := os.Stat(cleanPath)
	if statErr != nil || fileInfo.IsDir() {
		return errors.New("安装包文件不存在或不可读取")
	}
	pkg.FileSize = fileInfo.Size()
	if pkg.Mode == domain.InstallModeOffline {
		manifest, err := loadOfflineBundleManifest(pkg.FilePath)
		if err != nil {
			return err
		}
		if err := validateOfflineBundleManifest(pkg.FilePath, manifest); err != nil {
			return err
		}
		pkg.Manifest = manifest
		pkg.Offline = true
	}
	snapshot := s.store.Snapshot()
	if pkg.ImportedAt == "" {
		pkg.ImportedAt = time.Now().UTC().Format(time.RFC3339)
	}
	for index, item := range snapshot.InstallPackages {
		if item.ID == pkg.ID {
			if strings.TrimSpace(pkg.FilePath) == "" {
				pkg.FilePath = item.FilePath
				pkg.FileSize = item.FileSize
			}
			snapshot.InstallPackages[index] = pkg
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-install-package", pkg.Name, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.InstallPackages = append(snapshot.InstallPackages, pkg)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-install-package", pkg.Name, "success")
	return s.persistWithReport(snapshot)
}

func loadOfflineBundleManifest(path string) (domain.OfflineBundleManifest, error) {
	ext := strings.ToLower(path)
	if strings.HasSuffix(ext, ".json") {
		file, err := os.Open(path)
		if err != nil {
			return domain.OfflineBundleManifest{}, errors.New("离线资源包 manifest 不可读取")
		}
		defer file.Close()
		return decodeOfflineBundleManifest(file)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.OfflineBundleManifest{}, errors.New("离线资源包不可读取")
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(ext, ".zip") {
		info, statErr := file.Stat()
		if statErr != nil {
			return domain.OfflineBundleManifest{}, errors.New("离线资源包不可读取")
		}
		zipReader, zipErr := zip.NewReader(file, info.Size())
		if zipErr != nil {
			return domain.OfflineBundleManifest{}, errors.New("离线资源包 zip 格式无效")
		}
		for _, entry := range zipReader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			if filepath.Base(entry.Name) != "manifest.json" {
				continue
			}
			rc, openErr := entry.Open()
			if openErr != nil {
				return domain.OfflineBundleManifest{}, errors.New("离线资源包 manifest 不可读取")
			}
			defer rc.Close()
			return decodeOfflineBundleManifest(rc)
		}
		return domain.OfflineBundleManifest{}, errors.New("离线资源包缺少 manifest.json")
	}
	if strings.HasSuffix(ext, ".tgz") || strings.HasSuffix(ext, ".tar.gz") || strings.HasSuffix(ext, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return domain.OfflineBundleManifest{}, errors.New("离线资源包 gzip 格式无效")
		}
		defer gz.Close()
		reader = gz
	}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return domain.OfflineBundleManifest{}, errors.New("离线资源包 tar 格式无效")
		}
		name := filepath.Clean(header.Name)
		if header.Typeflag != tar.TypeReg || filepath.Base(name) != "manifest.json" {
			continue
		}
		return decodeOfflineBundleManifest(tarReader)
	}
	return domain.OfflineBundleManifest{}, errors.New("离线资源包缺少 manifest.json")
}

func decodeOfflineBundleManifest(reader io.Reader) (domain.OfflineBundleManifest, error) {
	var manifest domain.OfflineBundleManifest
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	if err := decoder.Decode(&manifest); err != nil {
		return domain.OfflineBundleManifest{}, fmt.Errorf("离线资源包 manifest JSON 无效: %w", err)
	}
	if err := validateOfflineBundleRequiredFields(manifest); err != nil {
		return domain.OfflineBundleManifest{}, err
	}
	return manifest, nil
}

func validateOfflineBundleRequiredFields(manifest domain.OfflineBundleManifest) error {
	if strings.TrimSpace(manifest.K3sVersion) == "" || strings.TrimSpace(manifest.RuntimeVersion) == "" || strings.TrimSpace(manifest.KubectlVersion) == "" {
		return errors.New("离线资源包 manifest 缺少 K3s、runtime 或 kubectl 版本")
	}
	if len(manifest.OSFamily) == 0 {
		return errors.New("离线资源包 manifest 缺少目标 OS 兼容性")
	}
	if len(manifest.SHA256) == 0 {
		return errors.New("离线资源包 manifest 缺少 SHA256 清单")
	}
	return nil
}

func validateOfflineBundleManifest(path string, manifest domain.OfflineBundleManifest) error {
	if !offlineManifestSupportsLinux(manifest.OSFamily) {
		return errors.New("离线资源包 manifest 不兼容 Linux 目标节点")
	}
	for name, expected := range manifest.SHA256 {
		if strings.TrimSpace(name) == "" || !isValidSHA256(expected) {
			return errors.New("离线资源包 manifest SHA256 清单无效")
		}
	}
	baseName := filepath.Base(path)
	if expected, ok := manifest.SHA256[baseName]; ok {
		actual, err := fileSHA256(path)
		if err != nil {
			return errors.New("离线资源包 SHA256 计算失败")
		}
		if !strings.EqualFold(actual, expected) {
			return errors.New("离线资源包 SHA256 校验失败")
		}
	} else if strings.HasSuffix(strings.ToLower(path), ".tar") || strings.HasSuffix(strings.ToLower(path), ".tgz") || strings.HasSuffix(strings.ToLower(path), ".tar.gz") || strings.HasSuffix(strings.ToLower(path), ".zip") {
		if err := validateOfflineBundleArchiveFiles(path, manifest.SHA256); err != nil {
			return err
		}
	}
	return nil
}

const maxArchiveEntrySize = 512 << 20

func validateOfflineBundleArchiveFiles(path string, expected map[string]string) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("离线资源包不可读取")
	}
	defer file.Close()
	lowerPath := strings.ToLower(path)
	remaining := map[string]string{}
	for name, hash := range expected {
		cleanName := filepath.Clean(name)
		if filepath.Base(cleanName) == "manifest.json" || filepath.Base(cleanName) == filepath.Base(path) {
			continue
		}
		remaining[cleanName] = hash
	}
	if len(remaining) == 0 {
		return nil
	}
	if strings.HasSuffix(lowerPath, ".zip") {
		return validateZipArchiveFiles(file, remaining)
	}
	var reader io.Reader = file
	if strings.HasSuffix(lowerPath, ".tgz") || strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return errors.New("离线资源包 gzip 格式无效")
		}
		defer gz.Close()
		reader = gz
	}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("离线资源包 tar 格式无效")
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		cleanName := filepath.Clean(header.Name)
		expectedHash, ok := remaining[cleanName]
		if !ok {
			expectedHash, ok = remaining[filepath.Base(cleanName)]
		}
		if !ok {
			continue
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, io.LimitReader(tarReader, maxArchiveEntrySize)); err != nil {
			return errors.New("离线资源包内容读取失败")
		}
		actual := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(actual, expectedHash) {
			return errors.New("离线资源包 SHA256 校验失败")
		}
		delete(remaining, cleanName)
		delete(remaining, filepath.Base(cleanName))
	}
	if len(remaining) > 0 {
		return errors.New("离线资源包缺少 manifest 声明的文件")
	}
	return nil
}

func validateZipArchiveFiles(file *os.File, expected map[string]string) error {
	info, err := file.Stat()
	if err != nil {
		return errors.New("离线资源包不可读取")
	}
	zipReader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return errors.New("离线资源包 zip 格式无效")
	}
	remaining := make(map[string]string, len(expected))
	for k, v := range expected {
		remaining[k] = v
	}
	for _, entry := range zipReader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		cleanName := filepath.Clean(entry.Name)
		expectedHash, ok := remaining[cleanName]
		if !ok {
			expectedHash, ok = remaining[filepath.Base(cleanName)]
		}
		if !ok {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return errors.New("离线资源包内容读取失败")
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, io.LimitReader(rc, maxArchiveEntrySize)); err != nil {
			rc.Close()
			return errors.New("离线资源包内容读取失败")
		}
		rc.Close()
		actual := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(actual, expectedHash) {
			return errors.New("离线资源包 SHA256 校验失败")
		}
		delete(remaining, cleanName)
		delete(remaining, filepath.Base(cleanName))
	}
	if len(remaining) > 0 {
		return errors.New("离线资源包缺少 manifest 声明的文件")
	}
	return nil
}

func offlineManifestSupportsLinux(families []string) bool {
	for _, family := range families {
		value := strings.ToLower(strings.TrimSpace(family))
		if value == "linux" || strings.HasPrefix(value, "ubuntu") || strings.HasPrefix(value, "debian") || strings.HasPrefix(value, "rhel") || strings.HasPrefix(value, "centos") || strings.HasPrefix(value, "rocky") || strings.HasPrefix(value, "almalinux") {
			return true
		}
	}
	return false
}

func isValidSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *PlatformService) DeleteInstallPackage(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.InstallPackage, 0, len(snapshot.InstallPackages))
	found := false
	for _, item := range snapshot.InstallPackages {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.InstallPackages = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-install-package", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ListMarketplaceApps() []domain.MarketplaceApp {
	return s.store.Snapshot().MarketplaceApps
}

func (s *PlatformService) SaveMarketplaceApp(app domain.MarketplaceApp) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(app.ID) == "" {
		app.ID = uniqueEntityID("app", app.Name)
	}
	if strings.TrimSpace(app.Name) == "" || strings.TrimSpace(app.Category) == "" || strings.TrimSpace(app.CurrentVersion) == "" {
		return errors.New("应用市场组件的名称、分类和当前版本不能为空")
	}
	if strings.Count(app.CurrentVersion, ".") < 2 {
		return errors.New("版本号格式无效，需符合 semver 格式如 1.0.0")
	}
	if app.Status != domain.MarketplaceDraft && app.Status != domain.MarketplaceOnShelf && app.Status != domain.MarketplaceOffShelf {
		return errors.New("应用市场组件状态无效")
	}
	if strings.TrimSpace(app.PackageName) == "" {
		return errors.New("上传包名称不能为空")
	}
	if strings.TrimSpace(app.PackageFile) == "" {
		return errors.New("应用包文件路径不能为空")
	}
	fileInfo, statErr := os.Stat(app.PackageFile)
	if statErr != nil || fileInfo.IsDir() {
		return errors.New("应用包文件不存在或不可读取")
	}
	app.PackageSize = fileInfo.Size()
	app.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	app.VersionHistory = normalizeVersionHistory(app.VersionHistory, app.CurrentVersion)
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.MarketplaceApps {
		if item.ID == app.ID {
			snapshot.MarketplaceApps[index] = app
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-marketplace-app", app.Name, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.MarketplaceApps = append(snapshot.MarketplaceApps, app)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-marketplace-app", app.Name, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteMarketplaceApp(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.MarketplaceApp, 0, len(snapshot.MarketplaceApps))
	found := false
	for _, item := range snapshot.MarketplaceApps {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.MarketplaceApps = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-marketplace-app", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) PublishMarketplaceApp(id string) error {
	return s.setMarketplaceAppStatus(id, domain.MarketplaceOnShelf, "publish-marketplace-app")
}

func (s *PlatformService) UnpublishMarketplaceApp(id string) error {
	return s.setMarketplaceAppStatus(id, domain.MarketplaceOffShelf, "unpublish-marketplace-app")
}

func (s *PlatformService) setMarketplaceAppStatus(id string, status domain.MarketplaceAppStatus, action string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.MarketplaceApps {
		if item.ID == id {
			snapshot.MarketplaceApps[index].Status = status
			snapshot.MarketplaceApps[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", action, id, "success")
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func (s *PlatformService) AddMarketplaceAppVersion(id string, version string, packageName string) error {
	version = strings.TrimSpace(version)
	packageName = strings.TrimSpace(packageName)
	if version == "" || packageName == "" {
		return errors.New("版本号和上传包名称不能为空")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.MarketplaceApps {
		if item.ID == id {
			item.CurrentVersion = version
			item.PackageName = packageName
			item.VersionHistory = normalizeVersionHistory(item.VersionHistory, version)
			item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			snapshot.MarketplaceApps[index] = item
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "add-marketplace-app-version", id, "success")
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func normalizeVersionHistory(history []string, current string) []string {
	result := make([]string, 0, len(history)+1)
	seen := map[string]bool{}
	for _, item := range append(history, current) {
		value := strings.TrimSpace(item)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (s *PlatformService) SaveClusterNode(node domain.ClusterNode) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	rawSSHHost := strings.TrimSpace(node.SSHHost)
	rawSSHUsername := strings.TrimSpace(node.SSHUsername)
	rawSSHPassword := strings.TrimSpace(node.SSHPassword)
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return errors.New("节点名称不能为空")
	}
	if strings.TrimSpace(node.InternalIP) == "" {
		return errors.New("节点内网 IP 不能为空")
	}
	if strings.TrimSpace(node.Role) == "" {
		node.Role = "worker"
	}
	if strings.TrimSpace(node.Status) == "" {
		node.Status = "ready"
	}
	if strings.TrimSpace(node.JoinMode) == "" {
		node.JoinMode = "ssh"
	}
	if node.SSHPort == 0 {
		node.SSHPort = 22
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snapshot := s.store.Snapshot()
	if strings.TrimSpace(node.ID) == "" {
		node.ID = "node-" + normalizeNodeIdentifier(name)
	}
	foundIndex := -1
	for i, item := range snapshot.ClusterNodes {
		if item.ID == node.ID {
			foundIndex = i
			break
		}
	}
	if foundIndex < 0 {
		for _, item := range snapshot.ClusterNodes {
			if strings.EqualFold(item.Name, name) {
				return errors.New("节点名称已存在")
			}
			if item.InternalIP == node.InternalIP {
				return errors.New("节点内网 IP 已存在")
			}
		}
	}
	if node.AutoProvision {
		if rawSSHHost == "" {
			return errors.New("自动装机需要填写 SSH 主机")
		}
		if node.SSHPort <= 0 {
			return errors.New("自动装机需要填写有效 SSH 端口")
		}
		if rawSSHUsername == "" {
			return errors.New("自动装机需要填写 SSH 用户名")
		}
		if rawSSHPassword == "" && (foundIndex < 0 || strings.TrimSpace(snapshot.ClusterNodes[foundIndex].SSHPasswordCiphertext) == "") {
			return errors.New("自动装机需要填写 SSH 登录密码")
		}
	}
	currentVersion := snapshot.ClusterUpgrade.CurrentVersion
	if currentVersion == "" {
		if len(snapshot.ClusterNodes) > 0 {
			currentVersion = snapshot.ClusterNodes[0].Version
		} else {
			currentVersion = "v1.28.0"
		}
	}
	if strings.TrimSpace(node.Version) == "" {
		node.Version = currentVersion
	}
	if strings.TrimSpace(node.OS) == "" {
		node.OS = "Ubuntu 22.04"
	}
	if strings.TrimSpace(node.Arch) == "" {
		node.Arch = "amd64"
	}
	if strings.TrimSpace(node.Kernel) == "" {
		node.Kernel = "5.15.0-generic"
	}
	if strings.TrimSpace(node.ContainerRuntime) == "" {
		node.ContainerRuntime = "containerd://1.7.x-k3s1"
	}
	if strings.TrimSpace(string(node.K3sRole)) == "" {
		node.K3sRole = domain.NodeK3sRole(node.Role)
	}
	if node.K3sRole != domain.K3sRoleControlPlane && node.K3sRole != domain.K3sRoleWorker {
		node.K3sRole = domain.K3sRoleWorker
	}
	if strings.TrimSpace(string(node.ProvisionMode)) == "" {
		node.ProvisionMode = domain.ProvisionModeOnline
	}
	if node.ProvisionMode != domain.ProvisionModeOnline && node.ProvisionMode != domain.ProvisionModeOffline {
		return errors.New("自动装机模式必须为 online 或 offline")
	}
	if strings.TrimSpace(node.ProvisionStatus) == "" {
		node.ProvisionStatus = string(domain.ProvisionPending)
	}
	if node.SGXStatus == "" {
		node.SGXStatus = domain.SGXUnknown
	}
	if node.RuntimeStatus == "" {
		node.RuntimeStatus = domain.RuntimeUnknown
	}
	if strings.TrimSpace(node.RuntimeClass) == "" {
		node.RuntimeClass = node.Role
	}
	if strings.TrimSpace(node.CapacityCPU) == "" {
		node.CapacityCPU = "8 vCPU"
	}
	if strings.TrimSpace(node.CapacityMemory) == "" {
		node.CapacityMemory = "16 GiB"
	}
	node.Labels = normalizeStringList(node.Labels)
	node.Taints = normalizeStringList(node.Taints)
	if node.ManagementIP == "" {
		node.ManagementIP = node.InternalIP
	}
	if strings.TrimSpace(node.SSHHost) == "" {
		node.SSHHost = node.ManagementIP
	}
	if strings.TrimSpace(node.SSHUsername) == "" {
		node.SSHUsername = "root"
	}
	if strings.TrimSpace(node.SSHPassword) == "" {
		if foundIndex < 0 {
			return errors.New("目标主机登录密码不能为空")
		}
		node.SSHPasswordCiphertext = snapshot.ClusterNodes[foundIndex].SSHPasswordCiphertext
		node.SSHPasswordConfigured = snapshot.ClusterNodes[foundIndex].SSHPasswordConfigured
	} else {
		if node.SSHPassword != "" {
			ciphertext, err := security.EncryptString(node.SSHPassword)
			if err != nil {
				return fmt.Errorf("目标主机密码加密失败: %w", err)
			}
			node.SSHPasswordCiphertext = ciphertext
			node.SSHPasswordConfigured = ciphertext != ""
		}
	}
	node.SSHPassword = ""
	if node.CPUUsage < 0 {
		node.CPUUsage = 0
	}
	if node.MemoryUsage < 0 {
		node.MemoryUsage = 0
	}
	if node.DiskUsage < 0 {
		node.DiskUsage = 0
	}
	if node.PodCount < 0 {
		node.PodCount = 0
	}
	if foundIndex < 0 {
		node.CPUUsage = 0
		node.MemoryUsage = 0
		node.DiskUsage = 0
		node.PodCount = 0
	}
	node.LastHeartbeat = now
	node.JoinedAt = now
	node.LastJoinAttemptAt = now
	joinCommand, joinMessage := buildJoinCommand(snapshot.ClusterNodes, node)
	node.LastJoinMessage = joinMessage
	node.JoinCommand = joinCommand
	if joinCommand == "" {
		node.JoinStatus = "credential_ready"
	} else {
		node.JoinStatus = "join_command_ready"
	}
	if node.AutoProvision {
		task, err := createProvisioningTaskInSnapshot(&snapshot, node, "platform", now)
		if err != nil {
			return err
		}
		node.ProvisionTaskID = task.ID
		node.ProvisionStatus = string(task.Status)
		node.RuntimeStatus = domain.RuntimePending
		if node.EnableSGX {
			node.SGXStatus = domain.SGXPending
		}
	}
	if foundIndex >= 0 {
		if snapshot.ClusterNodes[foundIndex].JoinedAt != "" {
			node.JoinedAt = snapshot.ClusterNodes[foundIndex].JoinedAt
		}
		if node.CPUUsage == 0 && snapshot.ClusterNodes[foundIndex].CPUUsage > 0 {
			node.CPUUsage = snapshot.ClusterNodes[foundIndex].CPUUsage
		}
		if node.MemoryUsage == 0 && snapshot.ClusterNodes[foundIndex].MemoryUsage > 0 {
			node.MemoryUsage = snapshot.ClusterNodes[foundIndex].MemoryUsage
		}
		if node.DiskUsage == 0 && snapshot.ClusterNodes[foundIndex].DiskUsage > 0 {
			node.DiskUsage = snapshot.ClusterNodes[foundIndex].DiskUsage
		}
		if node.PodCount == 0 && snapshot.ClusterNodes[foundIndex].PodCount > 0 {
			node.PodCount = snapshot.ClusterNodes[foundIndex].PodCount
		}
		snapshot.ClusterNodes[foundIndex] = node
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-cluster-node", node.Name, "success")
	} else {
		snapshot.ClusterNodes = append(snapshot.ClusterNodes, node)
		alertIDToken := strings.NewReplacer(".", "-", ":", "-", "+", "-", "T", "-", "Z", "").Replace(node.ID + "-" + now)
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-node-joined-" + alertIDToken, Level: "info", Source: node.Name, Message: fmt.Sprintf("新节点 %s 已保存接入凭据并完成预接入登记", node.Name), Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-node-joined-" + alertIDToken, NodeID: node.ID, Category: "node", Level: "info", Message: fmt.Sprintf("节点 %s 已进入真实接入预留状态，目标版本 %s", node.Name, node.Version), RecordedAt: now}}, snapshot.ClusterLogs...)
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-cluster-node", node.Name, "success")
	}
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) CreateProvisioningTask(node domain.ClusterNode, actor string) (domain.ProvisioningTask, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	task, err := createProvisioningTaskInSnapshot(&snapshot, node, actor, now)
	if err != nil {
		return domain.ProvisioningTask{}, err
	}
	for index := range snapshot.ClusterNodes {
		if snapshot.ClusterNodes[index].ID != node.ID {
			continue
		}
		snapshot.ClusterNodes[index].AutoProvision = true
		snapshot.ClusterNodes[index].ProvisionTaskID = task.ID
		snapshot.ClusterNodes[index].ProvisionStatus = string(task.Status)
		snapshot.ClusterNodes[index].RuntimeStatus = domain.RuntimePending
		if task.EnableSGX {
			snapshot.ClusterNodes[index].SGXStatus = domain.SGXPending
		}
		break
	}
	if err := s.persistWithReport(snapshot); err != nil {
		return domain.ProvisioningTask{}, err
	}
	return task, nil
}

func (s *PlatformService) RetryProvisioningTask(id string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	for taskIndex := range snapshot.ProvisioningTasks {
		task := snapshot.ProvisioningTasks[taskIndex]
		if task.ID != id {
			continue
		}
		if task.Status != domain.ProvisionFailed && task.Status != domain.ProvisionCancelled {
			return errors.New("仅失败或已取消的自动装机任务可重试")
		}
		resetStarted := false
		for stepIndex := range task.Steps {
			step := &task.Steps[stepIndex]
			if step.Status == domain.StepFailed || step.Status == domain.StepRunning || resetStarted {
				resetStarted = true
				step.Status = domain.StepPending
				step.StartedAt = ""
				step.FinishedAt = ""
				step.Message = "等待重试"
				step.Evidence = ""
			}
		}
		if !resetStarted && len(task.Steps) > 0 {
			task.Steps[0].Status = domain.StepPending
			task.Steps[0].Message = "等待重试"
		}
		task.Status = domain.ProvisionPending
		task.CurrentStep = firstPendingProvisioningStep(task.Steps)
		task.UpdatedAt = now
		task.CompletedAt = ""
		task.Message = "任务已提交重试"
		snapshot.ProvisioningTasks[taskIndex] = task
		updateNodeProvisioningStatus(snapshot.ClusterNodes, task.NodeID, task.ID, string(task.Status), task.EnableSGX)
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actorOrPlatform(actor), "retry-provisioning-task", task.ID, "success")
		return s.persistWithReport(snapshot)
	}
	return store.ErrNotFound
}

func (s *PlatformService) CancelProvisioningTask(id string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	for taskIndex := range snapshot.ProvisioningTasks {
		task := snapshot.ProvisioningTasks[taskIndex]
		if task.ID != id {
			continue
		}
		if task.Status == domain.ProvisionSucceeded || task.Status == domain.ProvisionCancelled {
			return errors.New("自动装机任务已结束")
		}
		for stepIndex := range task.Steps {
			if task.Steps[stepIndex].Status == domain.StepRunning || task.Steps[stepIndex].Status == domain.StepPending {
				task.Steps[stepIndex].Status = domain.StepSkipped
				task.Steps[stepIndex].FinishedAt = now
				task.Steps[stepIndex].Message = "任务已取消"
			}
		}
		task.Status = domain.ProvisionCancelled
		task.UpdatedAt = now
		task.CompletedAt = now
		task.Message = "任务已取消"
		snapshot.ProvisioningTasks[taskIndex] = task
		updateNodeProvisioningStatus(snapshot.ClusterNodes, task.NodeID, task.ID, string(task.Status), task.EnableSGX)
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actorOrPlatform(actor), "cancel-provisioning-task", task.ID, "success")
		return s.persistWithReport(snapshot)
	}
	return store.ErrNotFound
}

func (s *PlatformService) SaveProvisioningTaskStatus(task domain.ProvisioningTask) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("自动装机任务 ID 不能为空")
	}
	if task.UpdatedAt == "" {
		task.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	for index := range snapshot.ProvisioningTasks {
		if snapshot.ProvisioningTasks[index].ID != task.ID {
			continue
		}
		previous := snapshot.ProvisioningTasks[index]
		if previous.Status == domain.ProvisionCancelled && task.Status != domain.ProvisionCancelled {
			return errors.New("取消的任务无法恢复为其他状态")
		}
		task.CreatedAt = previous.CreatedAt
		if previous.EnableSGX {
			task.EnableSGX = true
		}
		snapshot.ProvisioningTasks[index] = task
		updateNodeProvisioningStatus(snapshot.ClusterNodes, task.NodeID, task.ID, string(task.Status), task.EnableSGX)
		snapshot.AuditEvents = appendProvisioningAuditEvents(snapshot.AuditEvents, previous, task)
		return s.persistWithReport(snapshot)
	}
	return store.ErrNotFound
}

func validOCIReference(ref string) bool {
	if ref == "" || len(ref) > 255 {
		return false
	}
	for _, r := range ref {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-', '/':
		default:
			return false
		}
	}
	return !strings.Contains(ref, "//") && ref[0] != '/' && ref[len(ref)-1] != '/'
}

func validOCITag(tag string) bool {
	if tag == "" || len(tag) > 128 {
		return false
	}
	for i, r := range tag {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-':
		default:
			if i == 0 {
				return false
			}
			return false
		}
	}
	return true
}

func createProvisioningTaskInSnapshot(snapshot *store.Snapshot, node domain.ClusterNode, actor string, now string) (domain.ProvisioningTask, error) {
	if strings.TrimSpace(node.ID) == "" {
		return domain.ProvisioningTask{}, errors.New("自动装机任务缺少节点 ID")
	}
	for _, task := range snapshot.ProvisioningTasks {
		if task.NodeID == node.ID && (task.Status == domain.ProvisionPending || task.Status == domain.ProvisionRunning) {
			return domain.ProvisioningTask{}, errors.New("该节点已有未完成的自动装机任务")
		}
	}
	mode := node.ProvisionMode
	if mode != domain.ProvisionModeOnline && mode != domain.ProvisionModeOffline {
		return domain.ProvisioningTask{}, errors.New("自动装机安装模式无效")
	}
	if mode == domain.ProvisionModeOffline {
		if err := ensureOfflineBundleAvailable(snapshot.InstallPackages, node.OfflineBundleID); err != nil {
			return domain.ProvisioningTask{}, err
		}
	}
	role := node.K3sRole
	if role == "" {
		role = domain.NodeK3sRole(node.Role)
	}
	if role != domain.K3sRoleControlPlane && role != domain.K3sRoleWorker {
		role = domain.K3sRoleWorker
	}
	steps := buildProvisioningSteps(role, node.EnableSGX)
	task := domain.ProvisioningTask{
		ID:              uniqueEntityID("prov", node.ID+"-"+now),
		NodeID:          node.ID,
		Actor:           actorOrPlatform(actor),
		Mode:            mode,
		Role:            role,
		EnableSGX:       node.EnableSGX,
		OfflineBundleID: node.OfflineBundleID,
		Status:          domain.ProvisionPending,
		CurrentStep:     firstPendingProvisioningStep(steps),
		Steps:           steps,
		CreatedAt:       now,
		UpdatedAt:       now,
		Message:         "自动装机任务已创建，等待执行器调度",
	}
	snapshot.ProvisioningTasks = append(snapshot.ProvisioningTasks, task)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, task.Actor, "create-provisioning-task", task.ID, "pending")
	return task, nil
}

func ensureOfflineBundleAvailable(packages []domain.InstallPackage, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("离线安装模式必须选择离线资源包")
	}
	if id == domain.OfflineBundleBuiltin {
		return validateBuiltInBundleAvailable()
	}
	for _, pkg := range packages {
		if pkg.ID == id && pkg.Mode == domain.InstallModeOffline && pkg.Offline && strings.TrimSpace(pkg.FilePath) != "" {
			return validateOfflineBundleManifest(pkg.FilePath, pkg.Manifest)
		}
	}
	return errors.New("指定的离线资源包不存在或未完成校验")
}

func validateBuiltInBundleAvailable() error {
	bundleDir := runtimebundle.ResolveBuiltInBundleDir()
	entries, err := os.ReadDir(bundleDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("内置运行时资源包目录不存在或为空: %s", bundleDir)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(bundleDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest domain.OfflineBundleManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		if len(manifest.SHA256) == 0 {
			return fmt.Errorf("内置运行时资源包 %s 的 manifest.sha256 为空,请先执行 fetch.sh 下载二进制文件", entry.Name())
		}
		for name, hash := range manifest.SHA256 {
			if !isValidSHA256(hash) {
				return fmt.Errorf("内置运行时资源包 %s 的 manifest.sha256 中 %s 的校验和无效", entry.Name(), name)
			}
		}
	}
	return nil
}

func buildComplianceCSV(report *domain.ComplianceReport) ([]byte, string, string, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"类别", "等级", "控制项编号", "控制项名称", "描述", "修复建议"}); err != nil {
		return nil, "", "", err
	}
	for _, f := range report.Findings {
		if err := w.Write([]string{f.Category, f.Level, f.ControlID, f.ControlName, f.Message, f.Recommendation}); err != nil {
			return nil, "", "", err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", "", err
	}
	filename := fmt.Sprintf("compliance-report-%s.csv", sanitizeFilename(report.ID))
	return buf.Bytes(), "text/csv; charset=utf-8", filename, nil
}

func buildComplianceHTML(report *domain.ComplianceReport) ([]byte, string, string, error) {
	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><title>`)
	buf.WriteString(html.EscapeString(report.Title))
	buf.WriteString(`</title><style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:system-ui,-apple-system,sans-serif;color:#1a1a2e;padding:48px;max-width:960px;margin:0 auto}
h1{font-size:24px;margin-bottom:8px;color:#16213e}
.meta{display:flex;gap:24px;margin-bottom:32px;color:#64748b;font-size:14px}
.meta span{background:#f0f4ff;padding:4px 12px;border-radius:6px}
.score{font-size:48px;font-weight:700;color:#0f3460;margin-bottom:8px}
.score-label{font-size:14px;color:#64748b;margin-bottom:32px}
table{width:100%;border-collapse:collapse;font-size:13px}
th{background:#16213e;color:#fff;padding:10px 12px;text-align:left;font-weight:600}
td{padding:10px 12px;border-bottom:1px solid #e2e8f0}
tr:nth-child(even){background:#f8fafc}
.level-high{color:#dc2626;font-weight:600}
.level-medium{color:#f59e0b;font-weight:600}
.level-low{color:#16a34a}
.footer{margin-top:40px;font-size:12px;color:#94a3b8;border-top:1px solid #e2e8f0;padding-top:16px}
@media print{body{padding:24px}.no-print{display:none}}
</style></head><body>`)
	buf.WriteString("<h1>" + html.EscapeString(report.Title) + "</h1>")
	buf.WriteString(`<div class="meta">`)
	buf.WriteString("<span>标准: " + html.EscapeString(report.Standard) + "</span>")
	buf.WriteString("<span>生成时间: " + html.EscapeString(report.GeneratedAt) + "</span>")
	buf.WriteString("<span>状态: " + html.EscapeString(report.Status) + "</span>")
	buf.WriteString(`</div><div class="score">` + fmt.Sprintf("%d", report.Score) + ` 分</div>`)
	buf.WriteString(`<div class="score-label">合规评分</div>`)
	buf.WriteString(`<table><thead><tr><th>类别</th><th>等级</th><th>控制项</th><th>控制项名称</th><th>描述</th><th>修复建议</th></tr></thead><tbody>`)
	for _, f := range report.Findings {
		levelClass := "level-low"
		if f.Level == "high" {
			levelClass = "level-high"
		} else if f.Level == "medium" {
			levelClass = "level-medium"
		}
		buf.WriteString("<tr>")
		buf.WriteString("<td>" + html.EscapeString(f.Category) + "</td>")
		buf.WriteString("<td class=\"" + levelClass + "\">" + html.EscapeString(f.Level) + "</td>")
		buf.WriteString("<td>" + html.EscapeString(f.ControlID) + "</td>")
		buf.WriteString("<td>" + html.EscapeString(f.ControlName) + "</td>")
		buf.WriteString("<td>" + html.EscapeString(f.Message) + "</td>")
		buf.WriteString("<td>" + html.EscapeString(f.Recommendation) + "</td>")
		buf.WriteString("</tr>")
	}
	buf.WriteString(`</tbody></table>`)
	buf.WriteString(`<div class="footer no-print">本报告由等保一体机安全管理平台自动生成，数据取自合规即时检查结果。使用浏览器「打印」功能可保存为 PDF。</div>`)
	buf.WriteString(`</body></html>`)
	filename := fmt.Sprintf("compliance-report-%s.html", sanitizeFilename(report.ID))
	return buf.Bytes(), "text/html; charset=utf-8", filename, nil
}

func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "-", "<", "-", ">", "-", "|", "-", " ", "-",
	)
	return r.Replace(s)
}

func buildProvisioningSteps(role domain.NodeK3sRole, enableSGX bool) []domain.ProvisioningStep {
	steps := []domain.ProvisioningStep{
		{Name: "preflight", Status: domain.StepPending, Message: "等待执行目标节点前置检查"},
	}
	if role == domain.K3sRoleControlPlane {
		steps = append(steps, domain.ProvisioningStep{Name: "k3s_server_install", Status: domain.StepPending, Message: "等待安装 K3s server"})
	} else {
		steps = append(steps, domain.ProvisioningStep{Name: "k3s_agent_install", Status: domain.StepPending, Message: "等待安装 K3s agent 并加入控制面"})
	}
	steps = append(steps, domain.ProvisioningStep{Name: "runtime_verify", Status: domain.StepPending, Message: "等待验证 kubectl 与 container runtime"})
	if enableSGX {
		steps = append(steps, domain.ProvisioningStep{Name: "sgx_dcap_install", Status: domain.StepPending, Message: "等待安装和验证 SGX/DCAP 工具链"})
	}
	steps = append(steps, domain.ProvisioningStep{Name: "final_verify", Status: domain.StepPending, Message: "等待执行节点最终健康检查"})
	return steps
}

func firstPendingProvisioningStep(steps []domain.ProvisioningStep) string {
	for _, step := range steps {
		if step.Status == domain.StepPending || step.Status == domain.StepRunning || step.Status == domain.StepFailed {
			return step.Name
		}
	}
	return ""
}

func appendProvisioningAuditEvents(events []domain.AuditEvent, previous domain.ProvisioningTask, current domain.ProvisioningTask) []domain.AuditEvent {
	actor := actorOrPlatform(current.Actor)
	if previous.Status != current.Status {
		action := "update-provisioning-task"
		if current.Status == domain.ProvisionSucceeded {
			action = "complete-provisioning-task"
		} else if current.Status == domain.ProvisionFailed {
			action = "fail-provisioning-task"
		} else if current.Status == domain.ProvisionCancelled {
			action = "cancel-provisioning-task"
		}
		events = appendAuditEvent(events, actor, action, current.ID, sanitizeAuditResult(string(current.Status)+" "+current.Message))
	}
	previousSteps := map[string]domain.ProvisioningStep{}
	for _, step := range previous.Steps {
		previousSteps[step.Name] = step
	}
	for _, step := range current.Steps {
		oldStep, ok := previousSteps[step.Name]
		if ok && oldStep.Status == step.Status && oldStep.Message == step.Message && oldStep.Evidence == step.Evidence {
			continue
		}
		if step.Status == domain.StepPending {
			continue
		}
		result := sanitizeAuditResult(fmt.Sprintf("%s %s %s", step.Status, step.Message, step.Evidence))
		events = appendAuditEvent(events, actor, "provisioning-step-"+string(step.Status), current.ID+":"+step.Name, result)
	}
	if previous.Status == current.Status {
		events = appendAuditEvent(events, actor, "update-provisioning-task", current.ID, sanitizeAuditResult(string(current.Status)+" "+current.Message))
	}
	return events
}

func sanitizeAuditResult(value string) string {
	value = sanitizeProvisioningEvidence(value)
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}

func updateNodeProvisioningStatus(nodes []domain.ClusterNode, nodeID string, taskID string, status string, enableSGX bool) {
	for index := range nodes {
		if nodes[index].ID != nodeID {
			continue
		}
		nodes[index].ProvisionTaskID = taskID
		nodes[index].ProvisionStatus = status
		if status == string(domain.ProvisionPending) || status == string(domain.ProvisionRunning) {
			nodes[index].RuntimeStatus = domain.RuntimePending
			if enableSGX {
				nodes[index].SGXStatus = domain.SGXPending
			}
		}
		if status == string(domain.ProvisionSucceeded) {
			nodes[index].Status = "ready"
			nodes[index].RuntimeStatus = domain.RuntimeReady
			if enableSGX {
				nodes[index].SGXStatus = domain.SGXReady
			}
		}
		if status == string(domain.ProvisionFailed) && enableSGX && nodes[index].SGXStatus != domain.SGXReady {
			nodes[index].SGXStatus = domain.SGXPending
		}
		return
	}
}

func actorOrPlatform(actor string) string {
	value := strings.TrimSpace(actor)
	if value == "" {
		return "platform"
	}
	return value
}

func (s *PlatformService) actorName(actor string) string {
	return actorOrPlatform(actor)
}

func (s *PlatformService) DeleteClusterNode(id string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for _, comp := range snapshot.Components {
		if comp.Status == domain.ComponentDeployed || comp.Status == domain.ComponentDeploying {
			return errors.New("存在部署中或已部署的组件，无法删除节点")
		}
	}
	updated := make([]domain.ClusterNode, 0, len(snapshot.ClusterNodes))
	found := false
	var deletedNodeName string
	for _, item := range snapshot.ClusterNodes {
		if item.ID == id {
			found = true
			deletedNodeName = item.Name
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.ClusterNodes = updated
	cleanProvisioningTasks := make([]domain.ProvisioningTask, 0, len(snapshot.ProvisioningTasks))
	for _, task := range snapshot.ProvisioningTasks {
		if task.NodeID != id {
			cleanProvisioningTasks = append(cleanProvisioningTasks, task)
		}
	}
	snapshot.ProvisioningTasks = cleanProvisioningTasks
	cleanEnclaveResources := make([]domain.EnclaveResource, 0, len(snapshot.EnclaveResources))
	for _, res := range snapshot.EnclaveResources {
		if res.NodeID != id {
			cleanEnclaveResources = append(cleanEnclaveResources, res)
		}
	}
	snapshot.EnclaveResources = cleanEnclaveResources
	cleanAlerts := make([]domain.ClusterAlert, 0, len(snapshot.ClusterAlerts))
	for _, alert := range snapshot.ClusterAlerts {
		if alert.Source != id && alert.Source != deletedNodeName {
			cleanAlerts = append(cleanAlerts, alert)
		}
	}
	snapshot.ClusterAlerts = cleanAlerts
	cleanLogs := make([]domain.ClusterLog, 0, len(snapshot.ClusterLogs))
	for _, log := range snapshot.ClusterLogs {
		if log.NodeID != id {
			cleanLogs = append(cleanLogs, log)
		}
	}
	snapshot.ClusterLogs = cleanLogs
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "delete-cluster-node", id, "success")
	return s.persistWithReport(snapshot)
}

func normalizeNodeIdentifier(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-").Replace(normalized)
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	return strings.Trim(normalized, "-")
}

func buildJoinCommand(nodes []domain.ClusterNode, node domain.ClusterNode) (string, string) {
	isControlPlane := node.Role == "control-plane" || node.K3sRole == domain.K3sRoleControlPlane
	token := strings.TrimSpace(os.Getenv("K3S_TOKEN"))
	server := ""
	for _, item := range nodes {
		if item.ID == node.ID || (item.Role != "control-plane" && item.K3sRole != domain.K3sRoleControlPlane) {
			continue
		}
		server = strings.TrimSpace(item.InternalIP)
		if server == "" {
			server = strings.TrimSpace(item.ManagementIP)
		}
		if server == "" {
			server = strings.TrimSpace(item.SSHHost)
		}
		if server != "" {
			break
		}
	}
	if endpoint := strings.TrimSpace(node.ControlPlaneEndpoint); endpoint != "" {
		server = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
		server = strings.TrimSuffix(server, ":6443")
	}
	if isControlPlane && server == "" {
		return "curl -sfL https://get.k3s.io | sh -s - server --cluster-init", "首个控制平面节点将以单 master 模式初始化，后续可通过 K3S_TOKEN 加入更多 master 或 worker"
	}
	if token == "" {
		return "", "已保存主机凭据，缺少 K3S_TOKEN，等待现场提供真实 join token"
	}
	if server == "" {
		return "", "已保存主机凭据，缺少控制平面地址，等待真实 K3s server 地址"
	}
	if isControlPlane {
		return fmt.Sprintf("curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=${K3S_TOKEN} sh -s - server", server), "新增控制平面节点将加入已有 master 组建高可用集群"
	}
	return fmt.Sprintf("curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=${K3S_TOKEN} sh -s - agent", server), "Worker 节点将加入已有控制平面扩展资源容量"
}

func normalizeStringList(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (s *PlatformService) SaveClusterQuota(quota domain.ClusterQuota) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(quota.ID) == "" {
		quota.ID = uniqueEntityID("quota", quota.Scope)
	}
	if strings.TrimSpace(quota.Scope) == "" || strings.TrimSpace(quota.CPULimit) == "" || strings.TrimSpace(quota.MemoryLimit) == "" {
		return errors.New("资源配额字段不能为空")
	}
	if !strings.HasSuffix(quota.CPULimit, "m") {
		return errors.New("CPU 配额必须以 m 为单位（如 500m）")
	}
	if !strings.HasSuffix(quota.MemoryLimit, "Mi") && !strings.HasSuffix(quota.MemoryLimit, "Gi") && !strings.HasSuffix(quota.MemoryLimit, "Ki") {
		return errors.New("内存配额格式无效，必须以 Mi、Gi 或 Ki 为单位")
	}
	if quota.PodLimit < 1 {
		return errors.New("Pod 配额必须大于 0")
	}
	snapshot := s.store.Snapshot()
	quota.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	for index, item := range snapshot.ClusterQuotas {
		if item.ID == quota.ID {
			snapshot.ClusterQuotas[index] = quota
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-cluster-quota", quota.Scope, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.ClusterQuotas = append(snapshot.ClusterQuotas, quota)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-cluster-quota", quota.Scope, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteClusterQuota(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.ClusterQuota, 0, len(snapshot.ClusterQuotas))
	found := false
	for _, item := range snapshot.ClusterQuotas {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.ClusterQuotas = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-cluster-quota", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) UpgradeCluster(request domain.ClusterUpgradeRequest) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(request.Version) == "" {
		return errors.New("目标版本不能为空")
	}
	snapshot := s.store.Snapshot()
	if snapshot.ClusterUpgrade.Status != "idle" {
		return errors.New("当前已有升级相关任务正在执行，请稍后再试")
	}
	currentVersion := "-"
	if len(snapshot.ClusterNodes) > 0 {
		currentVersion = snapshot.ClusterNodes[0].Version
	}
	now := time.Now().UTC().Format(time.RFC3339)
	upgradeToken := strings.NewReplacer(".", "-", ":", "-", "+", "-", "T", "-", "Z", "").Replace(request.Version + "-" + now)

	execOK, execMsg := s.executor.ExecuteUpgrade(request.Version)
	if execOK {
		snapshot.ClusterUpgrade = domain.ClusterUpgradeStatus{
			Status:         "running",
			CurrentVersion: currentVersion,
			TargetVersion:  request.Version,
			Progress:       5,
			StartedAt:      now,
			Message:        execMsg,
		}
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-upgrade-start-" + upgradeToken, Level: "info", Source: "cluster-upgrade", Message: fmt.Sprintf("升级已通过 SSH 发起，目标版本 %s", request.Version), Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-upgrade-start-" + upgradeToken, NodeID: "cluster", Category: "upgrade", Level: "info", Message: fmt.Sprintf("执行器已通过 SSH 发起升级，目标版本 %s", request.Version), RecordedAt: now}}, snapshot.ClusterLogs...)
	} else {
		snapshot.ClusterUpgrade = domain.ClusterUpgradeStatus{
			Status:         "pending_executor",
			CurrentVersion: currentVersion,
			TargetVersion:  request.Version,
			Progress:       0,
			StartedAt:      now,
			Message:        execMsg,
		}
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-upgrade-start-" + upgradeToken, Level: "info", Source: "cluster-upgrade", Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-upgrade-start-" + upgradeToken, NodeID: "cluster", Category: "upgrade", Level: "info", Message: fmt.Sprintf("升级请求已登记，目标版本 %s: %s", request.Version, execMsg), RecordedAt: now}}, snapshot.ClusterLogs...)
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "upgrade-cluster-requested", request.Version, "pending-executor")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DownloadClusterUpgrade(request domain.ClusterUpgradeRequest) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(request.Version) == "" {
		return errors.New("目标版本不能为空")
	}
	snapshot := s.store.Snapshot()
	if snapshot.ClusterUpgrade.Status != "idle" {
		return errors.New("当前已有升级相关任务正在执行，请稍后再试")
	}
	currentVersion := "-"
	if len(snapshot.ClusterNodes) > 0 {
		currentVersion = snapshot.ClusterNodes[0].Version
	}
	now := time.Now().UTC().Format(time.RFC3339)
	upgradeToken := strings.NewReplacer(".", "-", ":", "-", "+", "-", "T", "-", "Z", "").Replace(request.Version + "-" + now)
	execOK, execMsg := s.executor.ExecuteUpgradeDownload(request.Version)
	status := "downloaded"
	level := "info"
	if !execOK {
		status = "download_pending_executor"
		level = "warning"
	}
	snapshot.ClusterUpgrade = domain.ClusterUpgradeStatus{
		Status:         status,
		CurrentVersion: currentVersion,
		TargetVersion:  request.Version,
		Progress:       0,
		StartedAt:      now,
		Message:        execMsg,
	}
	snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-upgrade-download-" + upgradeToken, Level: level, Source: "cluster-upgrade", Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
	snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-upgrade-download-" + upgradeToken, NodeID: "cluster", Category: "upgrade", Level: level, Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "download-cluster-upgrade", request.Version, status)
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ResetClusterUpgrade() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	if snapshot.ClusterUpgrade.Status != "running" && snapshot.ClusterUpgrade.Status != "pending_executor" && snapshot.ClusterUpgrade.Status != "downloaded" && snapshot.ClusterUpgrade.Status != "download_pending_executor" {
		return errors.New("当前无活跃升级任务")
	}
	snapshot.ClusterUpgrade = domain.ClusterUpgradeStatus{
		Status:         "idle",
		CurrentVersion: snapshot.ClusterUpgrade.CurrentVersion,
		Message:        "升级已取消",
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "upgrade-cluster-cancelled", snapshot.ClusterUpgrade.CurrentVersion, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveAlertThreshold(cfg domain.AlertThresholdConfig) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if cfg.CPU < 1 || cfg.CPU > 100 || cfg.Mem < 1 || cfg.Pod < 1 {
		return errors.New("告警阈值必须大于 0")
	}
	snapshot := s.store.Snapshot()
	snapshot.AlertThreshold = cfg
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-alert-threshold", fmt.Sprintf("cpu=%d mem=%d pod=%d", cfg.CPU, cfg.Mem, cfg.Pod), "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveEnclaveKey(key domain.EnclaveKeyMaterial) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(key.ID) == "" {
		key.ID = uniqueEntityID("key", key.Name)
	}
	if strings.TrimSpace(key.Name) == "" || strings.TrimSpace(key.ComponentID) == "" || strings.TrimSpace(key.Algorithm) == "" {
		return errors.New("密钥材料字段不能为空")
	}
	snapshot := s.store.Snapshot()
	if s.findComponent(snapshot, key.ComponentID) == nil {
		return errors.New("关联组件不存在")
	}
	key.RotatedAt = time.Now().UTC().Format(time.RFC3339)
	for index, item := range snapshot.EnclaveKeys {
		if item.ID == key.ID {
			snapshot.EnclaveKeys[index] = key
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-enclave-key", key.Name, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.EnclaveKeys = append(snapshot.EnclaveKeys, key)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-enclave-key", key.Name, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteEnclaveKey(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.EnclaveKeyMaterial, 0, len(snapshot.EnclaveKeys))
	found := false
	for _, item := range snapshot.EnclaveKeys {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.EnclaveKeys = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-enclave-key", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveEnclaveResource(res domain.EnclaveResource) (domain.EnclaveResource, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(res.ID) == "" {
		res.ID = "sgx-" + res.NodeID
	}
	if strings.TrimSpace(res.NodeID) == "" {
		return res, errors.New("关联节点 ID 不能为空")
	}
	if res.EPCSizeMB < 64 {
		res.EPCSizeMB = 256
	}
	if strings.TrimSpace(res.Status) == "" {
		res.Status = "standby"
	}
	snapshot := s.store.Snapshot()
	found := false
	for i, item := range snapshot.EnclaveResources {
		if item.ID == res.ID {
			snapshot.EnclaveResources[i] = res
			found = true
			break
		}
	}
	if !found {
		for _, item := range snapshot.EnclaveResources {
			if item.NodeID == res.NodeID {
				return res, errors.New("该节点已存在可信资源配置，请使用已有配置的 ID 进行更新")
			}
		}
		snapshot.EnclaveResources = append(snapshot.EnclaveResources, res)
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "save-enclave-resource", res.NodeID, "success")
	return res, s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteEnclaveResource(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.EnclaveResource, 0, len(snapshot.EnclaveResources))
	found := false
	for _, item := range snapshot.EnclaveResources {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.EnclaveResources = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-enclave-resource", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) RunEnclaveInspection() ([]domain.EnclaveInspection, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	inspections := make([]domain.EnclaveInspection, 0, len(snapshot.Enclaves))
	for _, enclave := range snapshot.Enclaves {
		execStatus, execSummary := s.executor.ExecuteInspection(enclave.ComponentID)
		record := domain.EnclaveInspection{ID: "inspection-" + enclave.ComponentID, Target: enclave.ComponentID, Status: execStatus, Summary: execSummary, CheckedAt: now}
		inspections = append(inspections, record)
	}
	snapshot.EnclaveInspections = append(snapshot.EnclaveInspections, inspections...)
	if len(snapshot.EnclaveInspections) > 100 {
		snapshot.EnclaveInspections = snapshot.EnclaveInspections[len(snapshot.EnclaveInspections)-100:]
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "run-enclave-inspection", fmt.Sprintf("巡检%d台飞地", len(inspections)), "success")
	return inspections, s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveSecurityPolicyRule(rule domain.SecurityPolicyRule) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = uniqueEntityID("policy", rule.Name)
	}
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.Category) == "" {
		return errors.New("策略规则名称和分类不能为空")
	}
	if rule.Category != "network" && rule.Category != "runtime" && rule.Category != "identity" {
		return errors.New("策略规则分类无效，必须为 network、runtime 或 identity")
	}
	snapshot := s.store.Snapshot()
	rule.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	execOK, execMsg := false, "策略已禁用，未下发到执行器"
	if rule.Status != "disabled" {
		execOK, execMsg = s.executor.ExecuteSecurityPolicy(rule)
	}
	if execOK {
		rule.Status = "active"
	} else if rule.Status != "disabled" {
		rule.Status = "staged"
	}
	if !execOK && rule.Status != "disabled" {
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-policy-pending-" + rule.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: rule.Name, Message: execMsg, Status: "open", CreatedAt: rule.UpdatedAt}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-policy-pending-" + rule.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "security-policy", Level: "warning", Message: execMsg, RecordedAt: rule.UpdatedAt}}, snapshot.ClusterLogs...)
	}
	for index, item := range snapshot.SecurityPolicies {
		if item.ID == rule.ID {
			snapshot.SecurityPolicies[index] = rule
			result := "success"
			if !execOK && rule.Status != "disabled" {
				result = "pending-executor"
			}
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-security-policy", rule.Name, result)
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.SecurityPolicies = append(snapshot.SecurityPolicies, rule)
	result := "success"
	if !execOK && rule.Status != "disabled" {
		result = "pending-executor"
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-security-policy", rule.Name, result)
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteSecurityPolicyRule(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.SecurityPolicyRule, 0, len(snapshot.SecurityPolicies))
	found := false
	for _, item := range snapshot.SecurityPolicies {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.SecurityPolicies = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-security-policy", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveComplianceTask(task domain.ComplianceTask) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(task.ID) == "" {
		task.ID = uniqueEntityID("task", task.ControlID)
	}
	if strings.TrimSpace(task.ControlID) == "" || strings.TrimSpace(task.ControlName) == "" {
		return errors.New("整改任务控制项不能为空")
	}
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.ComplianceTasks {
		if item.ID == task.ID {
			snapshot.ComplianceTasks[index] = task
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-compliance-task", task.ControlID, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.ComplianceTasks = append(snapshot.ComplianceTasks, task)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-compliance-task", task.ControlID, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteComplianceTask(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.ComplianceTask, 0, len(snapshot.ComplianceTasks))
	found := false
	for _, item := range snapshot.ComplianceTasks {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.ComplianceTasks = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-compliance-task", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveSystemSetting(setting domain.SystemSetting, actor string) (domain.SystemSetting, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(setting.ID) == "" {
		setting.ID = uniqueEntityID("setting", setting.Name)
	}
	if strings.TrimSpace(setting.Name) == "" || strings.TrimSpace(setting.Category) == "" {
		return domain.SystemSetting{}, errors.New("系统设置名称和分类不能为空")
	}
	if err := validateSetting(setting.ID, setting.Value); err != nil {
		return domain.SystemSetting{}, err
	}
	snapshot := s.store.Snapshot()
	setting.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	for index, item := range snapshot.SystemSettings {
		if item.ID == setting.ID {
			snapshot.SystemSettings[index] = setting
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "update-system-setting", setting.ID, "success")
			if err := s.persistWithReport(snapshot); err != nil {
				return domain.SystemSetting{}, err
			}
			return setting, nil
		}
	}
	snapshot.SystemSettings = append(snapshot.SystemSettings, setting)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "create-system-setting", setting.ID, "success")
	if err := s.persistWithReport(snapshot); err != nil {
		return domain.SystemSetting{}, err
	}
	return setting, nil
}

func (s *PlatformService) DeleteSystemSetting(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.SystemSetting, 0, len(snapshot.SystemSettings))
	found := false
	for _, item := range snapshot.SystemSettings {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.SystemSettings = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-system-setting", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteComponent(id string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	var target *domain.ComponentDefinition
	for i := range snapshot.Components {
		if snapshot.Components[i].ID == id {
			target = &snapshot.Components[i]
			break
		}
	}
	if target == nil {
		return store.ErrNotFound
	}
	if target.Status == domain.ComponentDeployed || target.Status == domain.ComponentDeploying {
		return fmt.Errorf("%w: 已部署或正在部署的组件暂时无法删除", store.ErrConflict)
	}
	updated := make([]domain.ComponentDefinition, 0, len(snapshot.Components))
	for _, item := range snapshot.Components {
		if item.ID != id {
			updated = append(updated, item)
		}
	}
	snapshot.Components = updated
	filteredEnclaves := make([]domain.EnclaveProfile, 0, len(snapshot.Enclaves))
	for _, enclave := range snapshot.Enclaves {
		if enclave.ComponentID != id {
			filteredEnclaves = append(filteredEnclaves, enclave)
		}
	}
	snapshot.Enclaves = filteredEnclaves
	filteredAttestations := make([]domain.AttestationRecord, 0, len(snapshot.Attestations))
	for _, record := range snapshot.Attestations {
		if record.ComponentID != id {
			filteredAttestations = append(filteredAttestations, record)
		}
	}
	snapshot.Attestations = filteredAttestations
	filteredKeys := make([]domain.EnclaveKeyMaterial, 0, len(snapshot.EnclaveKeys))
	for _, key := range snapshot.EnclaveKeys {
		if key.ComponentID != id {
			filteredKeys = append(filteredKeys, key)
		}
	}
	snapshot.EnclaveKeys = filteredKeys
	for index := range snapshot.Networks {
		snapshot.Networks[index].AttachedComponents = removeString(snapshot.Networks[index].AttachedComponents, id)
	}
	cleanLinks := make([]domain.TopologyLink, 0, len(snapshot.TopoLinks))
	for _, link := range snapshot.TopoLinks {
		if link.Source != id && link.Target != id {
			cleanLinks = append(cleanLinks, link)
		}
	}
	snapshot.TopoLinks = cleanLinks
	cleanInspections := make([]domain.EnclaveInspection, 0, len(snapshot.EnclaveInspections))
	for _, inspection := range snapshot.EnclaveInspections {
		if inspection.Target != id {
			cleanInspections = append(cleanInspections, inspection)
		}
	}
	snapshot.EnclaveInspections = cleanInspections
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "delete-component", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeployComponent(id string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	index := slices.IndexFunc(snapshot.Components, func(item domain.ComponentDefinition) bool { return item.ID == id })
	if index < 0 {
		return store.ErrNotFound
	}
	component := snapshot.Components[index]
	if component.Status == domain.ComponentDeployed || component.Status == domain.ComponentDeploying {
		return errors.New("组件已部署或正在部署")
	}
	if strings.TrimSpace(component.Name) == "" {
		return errors.New("组件名称不能为空")
	}
	if component.Isolation == domain.IsolationEnclave && !s.enclaveExists(snapshot, component.ID) {
		return errors.New("飞地组件缺少飞地配置")
	}
	manifest, err := s.buildManifestFromSnapshot(snapshot, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	execOK, execMsg := s.executor.ExecuteManifest(manifest)
	if execOK {
		component.Status = domain.ComponentDeployed
		snapshot.Components[index] = component
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-deploy-" + component.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "info", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "deploy-component", id, "success")
	} else {
		component.Status = domain.ComponentDeploying
		snapshot.Components[index] = component
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-deploy-pending-" + component.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: component.Name, Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-deploy-pending-" + component.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "warning", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "deploy-component", id, "pending-executor")
	}
	if component.Isolation == domain.IsolationEnclave {
		snapshot.Attestations = upsertAttestation(snapshot.Attestations, domain.AttestationRecord{
			ID:              "att-" + component.ID,
			ComponentID:     component.ID,
			Measurement:     "MRENCLAVE:" + component.ID,
			Verifier:        "dcap-verifier",
			Status:          "pending",
			VerifiedAt:      now,
			Standard:        "等保2.0",
			ControlID:       "SM-2.0-CA-01",
			ControlName:     "可信计算环境核验",
			PolicyResult:    "等待外部证明结果",
			Evidence:        "部署请求已登记，等待真实 DCAP Quote 证据",
			SecretsReleased: false,
		})
	}
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ScaleComponent(id string, replicas int, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if replicas < 1 {
		return errors.New("副本数必须大于 0")
	}
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.Components {
		if item.ID == id {
			now := time.Now().UTC().Format(time.RFC3339)
			execOK, execMsg := s.executor.ExecuteScale(item.Namespace, item.ID, replicas)
			if execOK {
				snapshot.Components[index].Replicas = replicas
				snapshot.Components[index].Status = domain.ComponentDeployed
				snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-scale-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "info", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
				snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "scale-component", id, "success")
			} else {
				snapshot.Components[index].Status = domain.ComponentDeploying
				snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-scale-pending-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: item.Name, Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
				snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-scale-pending-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "warning", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
				snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, s.actorName(actor), "scale-component", id, "pending-executor")
			}
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func (s *PlatformService) UpgradeComponent(id string, targetVersion string, actor string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(targetVersion) == "" {
		return errors.New("目标版本不能为空")
	}
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.Components {
		if item.ID == id {
			image := s.findImage(snapshot, item.Image)
			if image == nil {
				return errors.New("组件镜像不存在")
			}
			now := time.Now().UTC().Format(time.RFC3339)
			imageRef := buildTargetImageRef(*image, targetVersion)
			execOK, execMsg := s.executor.ExecuteComponentUpgrade(item.Namespace, item.ID, imageRef)
			if execOK {
				snapshot.Components[index].Version = targetVersion
				snapshot.Components[index].Status = domain.ComponentDeployed
				snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-upgrade-component-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "info", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
				snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "upgrade-component", id, "success")
			} else {
				snapshot.Components[index].Status = domain.ComponentDeploying
				snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-upgrade-component-pending-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: item.Name, Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
				snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-upgrade-component-pending-" + item.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "deployment", Level: "warning", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
				snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "upgrade-component", id, "pending-executor")
			}
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func (s *PlatformService) BatchDeployComponents(ids []string, actor string) []domain.BatchResultItem {
	results := make([]domain.BatchResultItem, 0, len(ids))
	for _, id := range ids {
		err := s.DeployComponent(id, actor)
		name := id
		snapshot := s.store.Snapshot()
		for _, c := range snapshot.Components {
			if c.ID == id {
				name = c.Name
				break
			}
		}
		if err != nil {
			results = append(results, domain.BatchResultItem{ID: id, Name: name, Success: false, Message: err.Error()})
		} else {
			results = append(results, domain.BatchResultItem{ID: id, Name: name, Success: true, Message: "部署请求已提交"})
		}
	}
	if err := s.RecordAudit(actor, "batch-deploy", fmt.Sprintf("%d components", len(ids)), "completed"); err != nil {
		log.Printf("BatchDeployComponents: RecordAudit failed: %v", err)
	}
	return results
}

func (s *PlatformService) BatchScaleComponents(ids []string, replicas int, actor string) []domain.BatchResultItem {
	results := make([]domain.BatchResultItem, 0, len(ids))
	for _, id := range ids {
		err := s.ScaleComponent(id, replicas, actor)
		name := id
		snapshot := s.store.Snapshot()
		for _, c := range snapshot.Components {
			if c.ID == id {
				name = c.Name
				break
			}
		}
		if err != nil {
			results = append(results, domain.BatchResultItem{ID: id, Name: name, Success: false, Message: err.Error()})
		} else {
			results = append(results, domain.BatchResultItem{ID: id, Name: name, Success: true, Message: fmt.Sprintf("扩缩容至 %d 副本已提交", replicas)})
		}
	}
	if err := s.RecordAudit(actor, "batch-scale", fmt.Sprintf("%d components", len(ids)), "completed"); err != nil {
		log.Printf("BatchScaleComponents: RecordAudit failed: %v", err)
	}
	return results
}

func (s *PlatformService) ListPlugins() []domain.PluginDefinition {
	return s.store.Snapshot().Plugins
}

func (s *PlatformService) SavePlugin(plugin domain.PluginDefinition) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(plugin.Name) == "" {
		return errors.New("插件名称不能为空")
	}
	if strings.TrimSpace(plugin.ID) == "" {
		plugin.ID = uniqueEntityID("plugin", plugin.Name)
	}
	if strings.TrimSpace(string(plugin.Type)) == "" {
		plugin.Type = domain.PluginTypeMonitoring
	}
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	if plugin.CreatedAt == "" {
		plugin.CreatedAt = now
	}
	plugin.UpdatedAt = now
	if plugin.Status == "" {
		plugin.Status = domain.PluginEnabled
	}
	for i, p := range snapshot.Plugins {
		if p.ID == plugin.ID {
			snapshot.Plugins[i] = plugin
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-plugin", plugin.ID, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.Plugins = append(snapshot.Plugins, plugin)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-plugin", plugin.ID, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeletePlugin(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for i, p := range snapshot.Plugins {
		if p.ID == id {
			snapshot.Plugins = append(snapshot.Plugins[:i], snapshot.Plugins[i+1:]...)
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-plugin", id, "success")
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func (s *PlatformService) EnablePlugin(id string) error {
	return s.setPluginStatus(id, domain.PluginEnabled)
}

func (s *PlatformService) DisablePlugin(id string) error {
	return s.setPluginStatus(id, domain.PluginDisabled)
}

func (s *PlatformService) setPluginStatus(id string, status domain.PluginStatus) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for i, p := range snapshot.Plugins {
		if p.ID == id {
			snapshot.Plugins[i].Status = status
			snapshot.Plugins[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			action := "enable-plugin"
			if status == domain.PluginDisabled {
				action = "disable-plugin"
			}
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", action, id, "success")
			return s.persistWithReport(snapshot)
		}
	}
	return store.ErrNotFound
}

func (s *PlatformService) DispatchPluginHook(event string, payload map[string]string) {
	plugins := s.ListPlugins()
	var enabled []domain.PluginDefinition
	for _, plugin := range plugins {
		if plugin.Status == domain.PluginEnabled && strings.TrimSpace(plugin.Endpoint) != "" {
			enabled = append(enabled, plugin)
		}
	}
	if len(enabled) == 0 {
		return
	}
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for _, plugin := range enabled {
		sem <- struct{}{}
		wg.Add(1)
		go func(ep string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[plugin] webhook goroutine panic for %s: %v", ep, r)
				}
			}()
			if !isSafeEndpoint(ep) {
				log.Printf("[plugin] webhook endpoint blocked (private IP): %s", ep)
				return
			}
			body, _ := json.Marshal(map[string]interface{}{"event": event, "payload": payload, "timestamp": time.Now().UTC().Format(time.RFC3339)})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
			if err != nil {
				log.Printf("[plugin] webhook request creation failed for %s: %v", ep, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{
				Timeout: 10 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					if len(via) >= 5 {
						return errors.New("too many redirects")
					}
					if !isSafeEndpoint(req.URL.String()) {
						return errors.New("redirect to unsafe endpoint blocked")
					}
					return nil
				},
			}
			httpResp, err := client.Do(req)
			if err != nil {
				log.Printf("[plugin] webhook dispatch failed for %s: %v", ep, err)
				return
			}
			httpResp.Body.Close()
		}(plugin.Endpoint)
	}
	wg.Wait()
}

func isSafeEndpoint(rawURL string) bool {
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsPrivate() || ip.IsUnspecified() || isSpecialUseIP(ip) {
			return false
		}
		return true
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, resolvedIP := range ips {
		v4 := resolvedIP.To4()
		if v4 == nil {
			v4 = resolvedIP
		}
		if v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsLinkLocalMulticast() || v4.IsMulticast() || v4.IsPrivate() || v4.IsUnspecified() || isSpecialUseIP(v4) {
			return false
		}
	}
	return true
}

var cgnatCIDR = func() *net.IPNet {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr
}()

func isSpecialUseIP(ip net.IP) bool {
	if cgnatCIDR.Contains(ip) {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		for _, cidr := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "198.18.0.0/15"} {
			_, net, err := net.ParseCIDR(cidr)
			if err == nil && net.Contains(ip4) {
				return true
			}
		}
	} else {
		if strings.HasPrefix(ip.String(), "2001:db8:") {
			return true
		}
	}
	return false
}

func buildTargetImageRef(image domain.ImageAsset, targetVersion string) string {
	version := strings.TrimSpace(targetVersion)
	if strings.Contains(version, "/") {
		return version
	}
	return fmt.Sprintf("%s/%s:%s", image.Registry, image.Repository, version)
}

func (s *PlatformService) GenerateManifest(id string) (string, error) {
	return s.buildManifestFromSnapshot(s.store.Snapshot(), id)
}

func (s *PlatformService) buildManifestFromSnapshot(snapshot store.Snapshot, id string) (string, error) {
	componentIndex := slices.IndexFunc(snapshot.Components, func(item domain.ComponentDefinition) bool { return item.ID == id })
	if componentIndex < 0 {
		return "", store.ErrNotFound
	}
	component := snapshot.Components[componentIndex]
	image := s.findImage(snapshot, component.Image)
	if image == nil {
		return "", errors.New("组件镜像不存在")
	}
	name := sanifyYAML(component.ID)
	namespace := sanifyYAML(component.Namespace)
	isolation := string(component.Isolation)
	manifest := []string{
		"apiVersion: apps/v1",
		"kind: Deployment",
		"metadata:",
		fmt.Sprintf("  name: %s", name),
		fmt.Sprintf("  namespace: %s", namespace),
		"spec:",
		fmt.Sprintf("  replicas: %d", component.Replicas),
		"  selector:",
		"    matchLabels:",
		fmt.Sprintf("      app: %s", name),
		"  template:",
		"    metadata:",
		"      labels:",
		fmt.Sprintf("        app: %s", name),
		fmt.Sprintf("        isolation: %s", isolation),
	}
	annotations := make([]string, 0)
	if len(component.NetworkAttachments) > 0 {
		annotations = append(annotations, fmt.Sprintf("        k8s.v1.cni.cncf.io/networks: %s", strings.Join(component.NetworkAttachments, ",")))
	}
	if component.Isolation == domain.IsolationEnclave && s.enclaveProfileSgxEnabled(snapshot, component.ID) {
		annotations = append(annotations, "        sgx.enabled: 'true'")
	}
	if component.MtlsEnabled {
		annotations = append(annotations, "        linkerd.io/inject: enabled")
	}
	if len(annotations) > 0 {
		manifest = append(manifest, "      annotations:")
		manifest = append(manifest, annotations...)
	}
	manifest = append(manifest, "    spec:")
	policy := isolationPolicyForLevel(snapshot.IsolationPolicies, component.Isolation)
	if component.Isolation == domain.IsolationEnclave {
		runtimeClassName := "rune"
		if policy != nil && strings.TrimSpace(policy.RuntimeClass) != "" {
			runtimeClassName = policy.RuntimeClass
		}
		runtimeClassName = sanifyYAML(runtimeClassName)
		manifest = append(manifest,
			fmt.Sprintf("      runtimeClassName: %s", runtimeClassName),
			"      nodeSelector:",
			"        intel.feature.node.kubernetes.io/sgx: 'true'",
		)
	}
	manifest = append(manifest,
		"      containers:",
		"        - name: main",
		fmt.Sprintf("          image: %s/%s:%s", image.Registry, image.Repository, image.Tag),
		"          imagePullPolicy: IfNotPresent",
	)
	if policy != nil {
		if securityCtx := buildSecurityContext(policy); len(securityCtx) > 0 {
			manifest = append(manifest, securityCtx...)
		}
	}
	return strings.Join(manifest, "\n"), nil
}

func isolationPolicyForLevel(policies []domain.IsolationPolicy, level domain.IsolationLevel) *domain.IsolationPolicy {
	for i := range policies {
		if policies[i].Level == level {
			return &policies[i]
		}
	}
	return nil
}

func buildSecurityContext(policy *domain.IsolationPolicy) []string {
	if policy == nil {
		return nil
	}
	lines := []string{"          securityContext:"}
	added := false
	if policy.ReadonlyRootFS {
		lines = append(lines, "            readOnlyRootFilesystem: true")
		added = true
	}
	if len(policy.DropCapabilities) > 0 {
		lines = append(lines, "            capabilities:")
		lines = append(lines, "              drop:")
		for _, cap := range policy.DropCapabilities {
			lines = append(lines, fmt.Sprintf("                - %s", sanifyYAML(cap)))
		}
		added = true
	}
	if policy.RunAsNonRoot {
		lines = append(lines, "            runAsNonRoot: true")
		added = true
	}
	if policy.AllowPrivilegeEscalation != nil {
		lines = append(lines, fmt.Sprintf("            allowPrivilegeEscalation: %v", *policy.AllowPrivilegeEscalation))
		added = true
	}
	if strings.TrimSpace(policy.SeccompProfile) != "" {
		lines = append(lines, "            seccompProfile:")
		lines = append(lines, fmt.Sprintf("              type: %s", sanifyYAML(policy.SeccompProfile)))
		added = true
	}
	if strings.TrimSpace(policy.AppArmorProfile) != "" {
		lines = append(lines, "            appArmorProfile:")
		lines = append(lines, fmt.Sprintf("              type: %s", sanifyYAML(policy.AppArmorProfile)))
		added = true
	}
	if strings.TrimSpace(policy.SELinuxOptions) != "" {
		lines = append(lines, "            seLinuxOptions:")
		for _, opt := range strings.Split(policy.SELinuxOptions, ",") {
			kv := strings.SplitN(strings.TrimSpace(opt), "=", 2)
			if len(kv) == 2 {
				lines = append(lines, fmt.Sprintf("              %s: %s", sanifyYAML(strings.TrimSpace(kv[0])), sanifyYAML(strings.TrimSpace(kv[1]))))
			}
		}
		added = true
	}
	if !added {
		return nil
	}
	return lines
}

func (s *PlatformService) enclaveProfileSgxEnabled(snapshot store.Snapshot, componentID string) bool {
	for _, ep := range snapshot.Enclaves {
		if ep.ComponentID == componentID && ep.SgxEnabled {
			return true
		}
	}
	return false
}

func (s *PlatformService) ListNetworks() []domain.NetworkAttachment {
	return s.store.Snapshot().Networks
}

func (s *PlatformService) ListTopoLinks() []domain.TopologyLink {
	return s.store.Snapshot().TopoLinks
}

func (s *PlatformService) SaveTopoLink(link domain.TopologyLink) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(link.ID) == "" {
		link.ID = uniqueEntityID("topo-link", link.Source+"-"+link.Target)
	}
	if strings.TrimSpace(link.Source) == "" || strings.TrimSpace(link.Target) == "" {
		return errors.New("拓扑连线源和目标不能为空")
	}
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.TopoLinks {
		if item.ID == link.ID {
			snapshot.TopoLinks[index] = link
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-topo-link", link.ID, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.TopoLinks = append(snapshot.TopoLinks, link)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-topo-link", link.ID, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteTopoLink(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.TopologyLink, 0, len(snapshot.TopoLinks))
	found := false
	for _, item := range snapshot.TopoLinks {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.TopoLinks = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-topo-link", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ListTopoEgress() []domain.TopologyNode {
	return s.store.Snapshot().TopoEgress
}

func (s *PlatformService) SaveTopoEgress(node domain.TopologyNode) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(node.ID) == "" {
		return errors.New("拓扑出口节点 ID 不能为空")
	}
	if strings.TrimSpace(node.Label) == "" {
		return errors.New("拓扑出口节点标签不能为空")
	}
	snapshot := s.store.Snapshot()
	for index, item := range snapshot.TopoEgress {
		if item.ID == node.ID {
			snapshot.TopoEgress[index] = node
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-topo-egress", node.ID, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.TopoEgress = append(snapshot.TopoEgress, node)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-topo-egress", node.ID, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteTopoEgress(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updatedLinks := make([]domain.TopologyLink, 0, len(snapshot.TopoLinks))
	removedLinks := 0
	for _, link := range snapshot.TopoLinks {
		if link.Source == id || link.Target == id {
			removedLinks++
			continue
		}
		updatedLinks = append(updatedLinks, link)
	}
	snapshot.TopoLinks = updatedLinks
	updated := make([]domain.TopologyNode, 0, len(snapshot.TopoEgress))
	found := false
	for _, item := range snapshot.TopoEgress {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.TopoEgress = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-topo-egress", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) SaveNetwork(network domain.NetworkAttachment) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(network.ID) == "" {
		network.ID = uniqueEntityID("network", network.Name)
	}
	if strings.TrimSpace(network.Name) == "" {
		return errors.New("网络名称不能为空")
	}
	if strings.TrimSpace(network.Bridge) == "" {
		return errors.New("网桥名称不能为空")
	}
	if strings.TrimSpace(network.ParentNIC) == "" {
		return errors.New("父网卡不能为空")
	}
	if strings.TrimSpace(network.Subnet) == "" {
		return errors.New("子网地址不能为空")
	}
	if !strings.Contains(network.Subnet, ".") && !strings.Contains(network.Subnet, ":") {
		return errors.New("子网地址格式无效")
	}
	if strings.TrimSpace(network.Gateway) == "" {
		return errors.New("网关地址不能为空")
	}
	if !strings.Contains(network.Gateway, ".") && !strings.Contains(network.Gateway, ":") {
		return errors.New("网关地址格式无效")
	}
	if network.VLANID < 0 || network.VLANID > 4094 {
		return errors.New("VLAN ID 必须在 0 到 4094 之间")
	}
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range snapshot.Networks {
		if item.ID != network.ID && network.VLANID > 0 && item.VLANID == network.VLANID && item.ParentNIC == network.ParentNIC {
			return errors.New("相同网卡上的 VLAN 已存在")
		}
	}
	execOK, execMsg := s.executor.ExecuteNetwork(network)
	if !execOK {
		snapshot.ClusterAlerts = append([]domain.ClusterAlert{{ID: "alert-network-pending-" + network.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), Level: "warning", Source: network.Name, Message: execMsg, Status: "open", CreatedAt: now}}, snapshot.ClusterAlerts...)
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-network-pending-" + network.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "network", Level: "warning", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
	} else {
		snapshot.ClusterLogs = append([]domain.ClusterLog{{ID: "log-network-apply-" + network.ID + "-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano()), NodeID: "cluster", Category: "network", Level: "info", Message: execMsg, RecordedAt: now}}, snapshot.ClusterLogs...)
	}
	for index, item := range snapshot.Networks {
		if item.ID == network.ID {
			snapshot.Networks[index] = network
			result := "success"
			if !execOK {
				result = "pending-executor"
			}
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-network", network.Name, result)
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.Networks = append(snapshot.Networks, network)
	result := "success"
	if !execOK {
		result = "pending-executor"
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-network", network.Name, result)
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteNetwork(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	for _, component := range snapshot.Components {
		if slices.Contains(component.NetworkAttachments, id) {
			return fmt.Errorf("%w: 存在关联组件，网络暂时无法删除", store.ErrConflict)
		}
	}
	updated := make([]domain.NetworkAttachment, 0, len(snapshot.Networks))
	found := false
	for _, item := range snapshot.Networks {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.Networks = updated
	cleanLinks := make([]domain.TopologyLink, 0, len(snapshot.TopoLinks))
	for _, link := range snapshot.TopoLinks {
		if link.Source != id && link.Target != id {
			cleanLinks = append(cleanLinks, link)
		}
	}
	snapshot.TopoLinks = cleanLinks
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-network", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) ListEnclaves() []domain.EnclaveProfile {
	return s.store.Snapshot().Enclaves
}

func (s *PlatformService) SaveEnclave(profile domain.EnclaveProfile) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(profile.ID) == "" {
		profile.ID = uniqueEntityID("enclave", profile.ComponentID)
	}
	if strings.TrimSpace(profile.ComponentID) == "" {
		return errors.New("飞地配置组件 ID 不能为空")
	}
	snapshot := s.store.Snapshot()
	component := s.findComponent(snapshot, profile.ComponentID)
	if component == nil {
		return errors.New("飞地配置关联组件不存在")
	}
	if component.Isolation != domain.IsolationEnclave {
		return errors.New("组件隔离级别需要设置为 enclave")
	}
	for index, item := range snapshot.Enclaves {
		if item.ID == profile.ID {
			snapshot.Enclaves[index] = profile
			snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "update-enclave", profile.ComponentID, "success")
			return s.persistWithReport(snapshot)
		}
	}
	snapshot.Enclaves = append(snapshot.Enclaves, profile)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "create-enclave", profile.ComponentID, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) DeleteEnclave(id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	updated := make([]domain.EnclaveProfile, 0, len(snapshot.Enclaves))
	found := false
	for _, item := range snapshot.Enclaves {
		if item.ID == id {
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		return store.ErrNotFound
	}
	snapshot.Enclaves = updated
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, "platform", "delete-enclave", id, "success")
	return s.persistWithReport(snapshot)
}

func (s *PlatformService) RunAttestation(actor string) ([]domain.AttestationRecord, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	result := make([]domain.AttestationRecord, 0)
	for _, component := range snapshot.Components {
		if component.Isolation != domain.IsolationEnclave {
			continue
		}
		status := "failed"
		policyResult := "度量值未命中白名单"
		evidence := "缺少有效飞地配置"
		measurement := "MRENCLAVE:" + component.ID
		secretsReleased := false

		if s.enclaveExists(snapshot, component.ID) {
			var enclaveProfile domain.EnclaveProfile
			for _, enc := range snapshot.Enclaves {
				if enc.ComponentID == component.ID {
					enclaveProfile = enc
					break
				}
			}
			execStatus, execMeasurement, execEvidence := s.executor.ExecuteAttestation(component.ID, enclaveProfile)
			status = execStatus
			measurement = execMeasurement
			evidence = execEvidence
			switch execStatus {
			case "verified":
				policyResult = "远程证明通过，度量值命中白名单"
				secretsReleased = true
			case "failed":
				policyResult = "远程证明未通过"
			default:
				policyResult = "等待外部 DCAP Quote 证据"
			}
		}
		record := domain.AttestationRecord{
			ID:              "att-" + component.ID,
			ComponentID:     component.ID,
			Measurement:     measurement,
			Verifier:        "dcap-verifier",
			Status:          status,
			VerifiedAt:      time.Now().UTC().Format(time.RFC3339),
			Standard:        "等保2.0",
			ControlID:       "SM-2.0-CA-01",
			ControlName:     "可信计算环境核验",
			PolicyResult:    policyResult,
			Evidence:        evidence,
			SecretsReleased: secretsReleased,
		}
		snapshot.Attestations = upsertAttestation(snapshot.Attestations, record)
		result = append(result, record)
	}
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "run-attestation", fmt.Sprintf("已证明%d个飞地组件", len(result)), "success")
	if err := s.persistWithReport(snapshot); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PlatformService) SubmitAttestationResult(id string, payload domain.AttestationResultPayload, actor string) (domain.AttestationRecord, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	status := strings.TrimSpace(payload.Status)
	if status != "verified" && status != "failed" && status != "pending" {
		return domain.AttestationRecord{}, errors.New("证明状态无效")
	}
	snapshot := s.store.Snapshot()
	now := time.Now().UTC().Format(time.RFC3339)
	for index, record := range snapshot.Attestations {
		if record.ID != id {
			continue
		}
		record.Status = status
		record.VerifiedAt = now
		if strings.TrimSpace(payload.PolicyResult) != "" {
			record.PolicyResult = strings.TrimSpace(payload.PolicyResult)
		} else if status == "verified" {
			record.PolicyResult = "外部证明结果通过"
		} else if status == "failed" {
			record.PolicyResult = "外部证明结果未通过"
		}
		if strings.TrimSpace(payload.Evidence) != "" {
			record.Evidence = strings.TrimSpace(payload.Evidence)
		}
		record.SecretsReleased = record.SecretsReleased && status == "verified" && strings.Contains(strings.ToLower(record.Evidence), "dcap")
		snapshot.Attestations[index] = record
		snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "submit-attestation-result", id, status)
		if err := s.persistWithReport(snapshot); err != nil {
			return domain.AttestationRecord{}, err
		}
		return record, nil
	}
	return domain.AttestationRecord{}, store.ErrNotFound
}

func (s *PlatformService) ExportComplianceReport(actor, reportID, format string) ([]byte, string, string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()

	var report *domain.ComplianceReport
	for i := range snapshot.Reports {
		if snapshot.Reports[i].ID == reportID {
			r := snapshot.Reports[i]
			report = &r
			break
		}
	}
	if report == nil {
		return nil, "", "", store.ErrNotFound
	}

	switch format {
	case "csv":
		return buildComplianceCSV(report)
	case "html":
		return buildComplianceHTML(report)
	default:
		return nil, "", "", fmt.Errorf("unsupported export format: %s", format)
	}
}

func (s *PlatformService) RunCompliance(actor string) (domain.ComplianceReport, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	snapshot := s.store.Snapshot()
	report := s.buildComplianceReport(snapshot)
	report.ID = "report-manual-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	report.Title = "等保 2.0 手动合规检查"
	snapshot.Reports = prependReport(snapshot.Reports, report)
	snapshot.AuditEvents = appendAuditEvent(snapshot.AuditEvents, actor, "run-compliance", report.ID, "success")
	if err := s.store.Replace(snapshot); err != nil {
		return domain.ComplianceReport{}, err
	}
	return report, nil
}

func (s *PlatformService) persistWithReport(snapshot store.Snapshot) error {
	return s.store.Replace(trimSnapshotCollections(snapshot))
}

func trimSnapshotCollections(snapshot store.Snapshot) store.Snapshot {
	if len(snapshot.ClusterLogs) > maxClusterLogs {
		snapshot.ClusterLogs = snapshot.ClusterLogs[:maxClusterLogs]
	}
	if len(snapshot.ClusterAlerts) > maxClusterAlerts {
		snapshot.ClusterAlerts = snapshot.ClusterAlerts[:maxClusterAlerts]
	}
	return snapshot
}

func (s *PlatformService) buildComplianceReport(snapshot store.Snapshot) domain.ComplianceReport {
	findings := make([]domain.ComplianceFinding, 0)
	score := 100
	for _, image := range snapshot.Images {
		if !image.Signed {
			findings = append(findings, domain.ComplianceFinding{Category: "镜像签名", Level: "high", Message: fmt.Sprintf("[metadata] 镜像 %s 缺少签名", image.Name), ControlID: "SM-2.0-SC-01", ControlName: "软件完整性保护", Recommendation: "补充镜像签名并在导入前校验"})
			score -= 12
		}
		if !image.SBOM {
			findings = append(findings, domain.ComplianceFinding{Category: "镜像清单", Level: "medium", Message: fmt.Sprintf("[metadata] 镜像 %s 缺少 SBOM", image.Name), ControlID: "SM-2.0-AM-01", ControlName: "资产可视化管理", Recommendation: "补充 SBOM 并关联版本台账"})
			score -= 8
		}
		if image.Vulnerability == "high" {
			findings = append(findings, domain.ComplianceFinding{Category: "漏洞扫描", Level: "high", Message: fmt.Sprintf("[metadata] 镜像 %s 存在高危漏洞", image.Name), ControlID: "SM-2.0-TV-01", ControlName: "恶意代码和漏洞防护", Recommendation: "升级镜像或增加隔离补偿措施"})
			score -= 15
		}
	}
	for _, component := range snapshot.Components {
		if component.Isolation == domain.IsolationEnclave && !s.enclaveExists(snapshot, component.ID) {
			findings = append(findings, domain.ComplianceFinding{Category: "飞地配置", Level: "high", Message: fmt.Sprintf("[metadata] 组件 %s 缺少飞地配置", component.Name), ControlID: "SM-2.0-CA-01", ControlName: "可信计算环境核验", Recommendation: "为高等级组件绑定飞地配置与证明策略"})
			score -= 12
		}
		if component.Isolation == domain.IsolationEnclave && !s.attestationVerified(snapshot, component.ID) {
			findings = append(findings, domain.ComplianceFinding{Category: "远程证明", Level: "medium", Message: fmt.Sprintf("[executor] 组件 %s 尚未完成有效证明", component.Name), ControlID: "SM-2.0-CA-02", ControlName: "关键执行环境可信验证", Recommendation: "执行远程证明并核验度量基线"})
			score -= 10
		}
	}
	for _, network := range snapshot.Networks {
		if len(network.AttachedComponents) == 0 {
			findings = append(findings, domain.ComplianceFinding{Category: "网络安全", Level: "medium", Message: fmt.Sprintf("网络 %s 未挂载组件", network.Name), Recommendation: "为该网络挂载组件以实现接入安全控制"})
			score -= 5
		}
	}
	for _, policy := range snapshot.SecurityPolicies {
		if policy.Status == "staged" && policy.Mode == "enforce" {
			findings = append(findings, domain.ComplianceFinding{Category: "策略合规", Level: "medium", Message: fmt.Sprintf("策略 %s 处于待发布状态", policy.Name), Recommendation: "发布策略使其进入生效状态"})
			score -= 8
		}
	}
	provisionFindings, provisionPenalty := buildProvisioningComplianceFindings(snapshot)
	findings = append(findings, provisionFindings...)
	score -= provisionPenalty
	if s.executor != nil {
		executorFindings := s.executor.ExecuteComplianceChecks()
		for _, f := range executorFindings {
			findings = append(findings, f)
			switch f.Level {
			case "high":
				score -= 10
			case "medium":
				score -= 5
			case "low":
				score -= 2
			default:
				score -= 3
			}
		}
	}
	for _, catalog := range snapshot.CatalogItems {
		used := false
		for _, component := range snapshot.Components {
			if component.Name == catalog.Name && component.Isolation == catalog.IsolationRecommend {
				used = true
				break
			}
		}
		if !used {
			findings = append(findings, domain.ComplianceFinding{Category: "隔离策略", Level: "low", Message: fmt.Sprintf("推荐隔离策略 %s 未被采纳", catalog.Name), Recommendation: "按推荐级别设置组件隔离策略"})
			score -= 3
		}
	}
	if score < 0 {
		score = 0
	}
	if len(findings) == 0 {
		findings = append(findings, domain.ComplianceFinding{Category: "综合状态", Level: "low", Message: "当前等保 2.0 基线项满足平台内建检查规则", ControlID: "SM-2.0-BASE", ControlName: "综合基线", Recommendation: "保持定期复核与证据留存"})
	}
	return domain.ComplianceReport{
		ID:          "report-latest",
		Title:       "等保 2.0 合规即时检查",
		Score:       score,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Status:      "ready",
		Standard:    "等保2.0",
		Findings:    findings,
	}
}

func mergeLatestReport(reports []domain.ComplianceReport, report domain.ComplianceReport) []domain.ComplianceReport {
	filtered := make([]domain.ComplianceReport, 0, len(reports))
	for _, item := range reports {
		if item.ID != "report-latest" {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) > 5 {
		filtered = filtered[:5]
	}
	return append([]domain.ComplianceReport{report}, filtered...)
}

func prependReport(reports []domain.ComplianceReport, report domain.ComplianceReport) []domain.ComplianceReport {
	updated := append([]domain.ComplianceReport{report}, reports...)
	if len(updated) > 6 {
		updated = updated[:6]
	}
	return updated
}

func buildProvisioningComplianceFindings(snapshot store.Snapshot) ([]domain.ComplianceFinding, int) {
	findings := make([]domain.ComplianceFinding, 0)
	penalty := 0
	for _, task := range snapshot.ProvisioningTasks {
		nodeName := provisioningNodeName(snapshot.ClusterNodes, task.NodeID)
		if task.Status == domain.ProvisionSucceeded {
			if evidence := provisioningEvidenceSummary(task, "k3s_server_install", "k3s_agent_install", "runtime_verify", "final_verify"); evidence != "" {
				findings = append(findings, domain.ComplianceFinding{Category: "自动装机证据", Level: "low", Message: fmt.Sprintf("[executor] 节点 %s 的 K3s/runtime 已完成真实验证：%s", nodeName, evidence), ControlID: "SM-2.0-AUD-01", ControlName: "自动装机过程证据", Recommendation: "保留该任务证据并纳入交付验收归档"})
			}
			if task.EnableSGX {
				if evidence := provisioningEvidenceSummary(task, "sgx_dcap_install"); evidence != "" {
					findings = append(findings, domain.ComplianceFinding{Category: "SGX/DCAP 证据", Level: "low", Message: fmt.Sprintf("[executor] 节点 %s 的 SGX/DCAP 已完成真实验证：%s", nodeName, evidence), ControlID: "SM-2.0-CA-03", ControlName: "可信计算节点验证", Recommendation: "保留 SGX/DCAP 验证输出并定期复核"})
				}
			}
			continue
		}
		if task.Status == domain.ProvisionFailed || task.Status == domain.ProvisionCancelled {
			findings = append(findings, domain.ComplianceFinding{Category: "自动装机证据", Level: "medium", Message: fmt.Sprintf("[executor] 节点 %s 的自动装机任务未完成：%s", nodeName, sanitizeProvisioningEvidence(task.Message)), ControlID: "SM-2.0-AUD-02", ControlName: "自动装机闭环", Recommendation: "查看任务失败阶段并完成重试或归档处置原因"})
			penalty += 8
		}
	}
	return findings, penalty
}

func provisioningNodeName(nodes []domain.ClusterNode, nodeID string) string {
	for _, node := range nodes {
		if node.ID == nodeID {
			if strings.TrimSpace(node.Name) != "" {
				return node.Name
			}
			return node.ID
		}
	}
	return nodeID
}

func provisioningEvidenceSummary(task domain.ProvisioningTask, names ...string) string {
	nameSet := map[string]bool{}
	for _, name := range names {
		nameSet[name] = true
	}
	parts := make([]string, 0, len(names))
	for _, step := range task.Steps {
		if !nameSet[step.Name] || step.Status != domain.StepSucceeded || strings.TrimSpace(step.Evidence) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", step.Name, sanitizeProvisioningEvidence(step.Evidence)))
	}
	result := strings.Join(parts, "; ")
	if len(result) > 360 {
		result = result[:360]
	}
	return result
}

func sanitizeProvisioningEvidence(value string) string {
	value = strings.TrimSpace(value)
	keys := []string{"K3S_TOKEN", "token", "password", "passwd", "secret"}
	for _, key := range keys {
		value = sanitizeProvisioningKeyValue(value, key)
	}
	return value
}

func sanitizeProvisioningKeyValue(value string, key string) string {
	for _, sep := range []string{"=", ":"} {
		needle := key + sep
		for {
			lowerValue := strings.ToLower(value)
			idx := strings.Index(lowerValue, strings.ToLower(needle))
			if idx < 0 {
				break
			}
			start := idx + len(needle)
			end := start
			for end < len(value) && !strings.ContainsRune(" \n\r\t;,&", rune(value[end])) {
				end++
			}
			if value[start:end] == "***" {
				break
			}
			value = value[:start] + "***" + value[end:]
		}
	}
	return value
}

func appendAuditEvent(events []domain.AuditEvent, actor string, action string, target string, result string) []domain.AuditEvent {
	now := time.Now().UTC().Format(time.RFC3339)
	entry := domain.AuditEvent{ID: fmt.Sprintf("audit-%d", time.Now().UTC().UnixNano()), Actor: actor, Action: action, Target: target, Result: result, CreatedAt: now}
	return append([]domain.AuditEvent{entry}, events...)
}

func (s *PlatformService) imageExists(snapshot store.Snapshot, id string) bool {
	return s.findImage(snapshot, id) != nil
}

func (s *PlatformService) networkExists(snapshot store.Snapshot, id string) bool {
	for _, item := range snapshot.Networks {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *PlatformService) enclaveExists(snapshot store.Snapshot, componentID string) bool {
	for _, item := range snapshot.Enclaves {
		if item.ComponentID == componentID {
			return true
		}
	}
	return false
}

func (s *PlatformService) attestationVerified(snapshot store.Snapshot, componentID string) bool {
	for _, item := range snapshot.Attestations {
		if item.ComponentID == componentID && item.Status == "verified" {
			return true
		}
	}
	return false
}

func (s *PlatformService) findImage(snapshot store.Snapshot, id string) *domain.ImageAsset {
	for _, item := range snapshot.Images {
		if item.ID == id {
			copied := item
			return &copied
		}
	}
	return nil
}

func (s *PlatformService) findComponent(snapshot store.Snapshot, id string) *domain.ComponentDefinition {
	for _, item := range snapshot.Components {
		if item.ID == id {
			copied := item
			return &copied
		}
	}
	return nil
}

func (s *PlatformService) attachComponentRelations(snapshot *store.Snapshot, component domain.ComponentDefinition) {
	for index := range snapshot.Networks {
		attached := slices.Contains(component.NetworkAttachments, snapshot.Networks[index].ID)
		contains := slices.Contains(snapshot.Networks[index].AttachedComponents, component.ID)
		if attached && !contains {
			snapshot.Networks[index].AttachedComponents = append(snapshot.Networks[index].AttachedComponents, component.ID)
		}
		if !attached && contains {
			snapshot.Networks[index].AttachedComponents = removeString(snapshot.Networks[index].AttachedComponents, component.ID)
		}
	}
}

func removeString(items []string, target string) []string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func upsertAttestation(items []domain.AttestationRecord, record domain.AttestationRecord) []domain.AttestationRecord {
	for index, item := range items {
		if item.ID == record.ID {
			items[index] = record
			return items
		}
	}
	return append(items, record)
}

func uniqueEntityID(prefix string, value string) string {
	base := normalizeNodeIdentifier(value)
	if base == "" {
		base = fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return prefix + "-" + base
}

func toUserView(user domain.User) domain.UserView {
	return domain.UserView{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
		LastLoginAt: user.LastLoginAt,
	}
}

func validateSetting(id string, value string) error {
	switch {
	case strings.HasSuffix(id, "-timeout") || strings.HasSuffix(id, "-interval"):
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("时间格式无效，示例：30m、1h")
		}
		if d < time.Minute {
			return errors.New("时间间隔不能小于 1 分钟")
		}
	case strings.Contains(id, "max") || strings.Contains(id, "limit"):
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil || n <= 0 {
			return errors.New("数值必须为正整数")
		}
	case strings.HasSuffix(id, "-enabled") || strings.HasSuffix(id, "-mode"):
		allowed := map[string]bool{"true": true, "false": true, "strict": true, "permissive": true, "audit": true}
		if !allowed[value] {
			return errors.New("取值无效，允许：true, false, strict, permissive, audit")
		}
	}
	return nil
}

func sanifyYAML(s string) string {
	cleaned := strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
	for _, r := range cleaned {
		if !unicode.IsPrint(r) && r != '\t' {
			cleaned = strings.ReplaceAll(cleaned, string(r), "")
		}
	}
	if strings.ContainsAny(cleaned, ":#{}[]&*!>|%@`\"'") || strings.HasPrefix(cleaned, "- ") || cleaned == "" {
		return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(cleaned) + "\""
	}
	return cleaned
}
