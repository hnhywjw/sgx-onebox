package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/service"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/store"
)

var errForbidden = errors.New("当前账号缺少访问权限")

type captchaChallenge struct {
	Answer    string
	ExpiresAt time.Time
}

type rateLimitEntry struct {
	count      int
	windowStart time.Time
}

type Server struct {
	service             *service.PlatformService
	captchaM            sync.Mutex
	captchas            map[string]captchaChallenge
	rateLimitM          sync.Mutex
	rateLimits          map[string]*rateLimitEntry
	lastRateLimitCleanup time.Time
	k3sCache *struct {
		mu    sync.RWMutex
		items map[string]struct {
			versions []string
			expires  time.Time
		}
	}
}

func NewServer() *Server {
	sqliteStore, err := store.NewSQLiteStore()
	if err != nil {
		panic("failed to create SQLite store: " + err.Error())
	}
	svc := service.NewPlatformService(sqliteStore)
	svc.StartSessionCleanup()
	srv := &Server{service: svc, captchas: map[string]captchaChallenge{}, rateLimits: map[string]*rateLimitEntry{}}
	srv.k3sCache = &struct {
		mu    sync.RWMutex
		items map[string]struct {
			versions []string
			expires  time.Time
		}
	}{items: make(map[string]struct {
		versions []string
		expires  time.Time
	})}
	return srv
}

func (s *Server) Shutdown() {
	s.service.Stop()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/upload", s.handleFileUpload)
	mux.HandleFunc("/api/v1/auth/captcha", s.handleCaptcha)
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", s.handleMe)
	mux.HandleFunc("/api/v1/auth/password", s.handleChangePassword)
	mux.HandleFunc("/api/v1/dashboard", s.handleDashboard)
	mux.HandleFunc("/api/v1/users", s.handleUsers)
	mux.HandleFunc("/api/v1/users/", s.handleUserByID)
	mux.HandleFunc("/api/v1/images/build", s.handleImageBuild)
	mux.HandleFunc("/api/v1/images", s.handleImages)
	mux.HandleFunc("/api/v1/images/", s.handleImageByID)
	mux.HandleFunc("/api/v1/components", s.handleComponents)
	mux.HandleFunc("/api/v1/components/batch/deploy", s.handleBatchDeploy)
	mux.HandleFunc("/api/v1/components/batch/scale", s.handleBatchScale)
	mux.HandleFunc("/api/v1/components/", s.handleComponentByID)
	mux.HandleFunc("/api/v1/networks", s.handleNetworks)
	mux.HandleFunc("/api/v1/networks/", s.handleNetworkByID)
	mux.HandleFunc("/api/v1/enclaves", s.handleEnclaves)
	mux.HandleFunc("/api/v1/enclaves/", s.handleEnclaveByID)
	mux.HandleFunc("/api/v1/attestations/run", s.handleRunAttestation)
	mux.HandleFunc("/api/v1/attestations", s.handleAttestations)
	mux.HandleFunc("/api/v1/attestations/", s.handleAttestationByID)
	mux.HandleFunc("/api/v1/install-packages", s.handleInstallPackages)
	mux.HandleFunc("/api/v1/install-packages/", s.handleInstallPackageByID)
	mux.HandleFunc("/api/v1/marketplace-apps", s.handleMarketplaceApps)
	mux.HandleFunc("/api/v1/marketplace-apps/", s.handleMarketplaceAppByID)
	mux.HandleFunc("/api/v1/provisioning-tasks/", s.handleProvisioningTaskByID)
	mux.HandleFunc("/api/v1/provisioning-tasks", s.handleProvisioningTasks)
	mux.HandleFunc("/api/v1/audit", s.handleAuditEvents)
	mux.HandleFunc("/api/v1/cluster/nodes", s.handleClusterNodes)
	mux.HandleFunc("/api/v1/cluster/nodes/", s.handleClusterNodeByID)
	mux.HandleFunc("/api/v1/cluster/quotas", s.handleClusterQuotas)
	mux.HandleFunc("/api/v1/cluster/quotas/", s.handleClusterQuotaByID)
	mux.HandleFunc("/api/v1/cluster/upgrade", s.handleClusterUpgrade)
	mux.HandleFunc("/api/v1/cluster/upgrade/download", s.handleClusterUpgradeDownload)
	mux.HandleFunc("/api/v1/cluster/alert-threshold", s.handleAlertThreshold)
	mux.HandleFunc("/api/v1/enclave-keys", s.handleEnclaveKeys)
	mux.HandleFunc("/api/v1/enclave-keys/", s.handleEnclaveKeyByID)
	mux.HandleFunc("/api/v1/enclave-resources", s.handleEnclaveResources)
	mux.HandleFunc("/api/v1/enclave-resources/", s.handleEnclaveResourceByID)
	mux.HandleFunc("/api/v1/enclave-inspections/run", s.handleRunEnclaveInspection)
	mux.HandleFunc("/api/v1/enclave-inspections", s.handleEnclaveInspections)
	mux.HandleFunc("/api/v1/enclave-inspections/", s.handleEnclaveInspectionByID)
	mux.HandleFunc("/api/v1/security-policies", s.handleSecurityPolicies)
	mux.HandleFunc("/api/v1/security-policies/", s.handleSecurityPolicyByID)
	mux.HandleFunc("/api/v1/compliance-tasks", s.handleComplianceTasks)
	mux.HandleFunc("/api/v1/compliance-tasks/", s.handleComplianceTaskByID)
	mux.HandleFunc("/api/v1/system-settings", s.handleSystemSettings)
	mux.HandleFunc("/api/v1/system-settings/", s.handleSystemSettingByID)
	mux.HandleFunc("/api/v1/compliance/run", s.handleRunCompliance)
	mux.HandleFunc("/api/v1/compliance/export", s.handleComplianceExport)
	mux.HandleFunc("/api/v1/plugins", s.handlePlugins)
	mux.HandleFunc("/api/v1/plugins/", s.handlePluginByID)
	mux.HandleFunc("/api/v1/topo/links", s.handleTopoLinks)
	mux.HandleFunc("/api/v1/topo/links/", s.handleTopoLinkByID)
	mux.HandleFunc("/api/v1/topo/egress", s.handleTopoEgress)
	mux.HandleFunc("/api/v1/topo/egress/", s.handleTopoEgressByID)
	mux.HandleFunc("/api/v1/cluster/k3s-versions", s.handleK3sVersions)
	return withCORS(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if err := s.service.Health(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "db": "disconnected"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": "connected"})
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20)

	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("无法解析上传表单"))
		return
	}

	allowedExtensions := map[string]bool{
		".tgz": true, ".tar.gz": true, ".tar": true, ".gz": true, ".zip": true,
		".iso": true, ".img": true, ".yaml": true, ".yml": true, ".json": true,
	}

	var uploadType string
	var dst *os.File
	var destPath string
	var safeName string
	var written int64
	cleanup := func() {
		if dst != nil {
			dst.Close()
			os.Remove(destPath)
		}
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			writeError(w, http.StatusBadRequest, errors.New("上传数据读取失败"))
			return
		}
		formName := part.FormName()

		if formName == "type" {
			var buf bytes.Buffer
			io.CopyN(&buf, part, 1024)
			uploadType = strings.TrimSpace(buf.String())
			part.Close()
			continue
		}

		if formName != "file" {
			part.Close()
			continue
		}

		filename := part.FileName()
		if filename == "" {
			part.Close()
			continue
		}

		ext := ""
		if strings.HasSuffix(filename, ".tar.gz") {
			ext = ".tar.gz"
		} else {
			dot := strings.LastIndex(filename, ".")
			if dot >= 0 {
				ext = filename[dot:]
			}
		}
		if ext == "" || !allowedExtensions[ext] {
			part.Close()
			writeError(w, http.StatusBadRequest, errors.New("不支持的文件类型，仅允许 tgz/tar.gz/tar/gz/zip/iso/img/yaml/yml/json"))
			return
		}

		if uploadType == "" {
			uploadType = "packages"
		}
		if !isValidUploadType(uploadType) {
			uploadType = "packages"
		}

		uploadDir := filepath.Join("data", "uploads", uploadType)
		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			part.Close()
			writeError(w, http.StatusInternalServerError, errors.New("创建上传目录失败"))
			return
		}
		safeName = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, filename)
		destPath = filepath.Join(uploadDir, safeName)
		if _, err := os.Stat(destPath); err == nil {
			part.Close()
			writeError(w, http.StatusConflict, errors.New("同名文件已存在，请重命名后重新上传"))
			return
		}
		dst, err = os.Create(destPath)
		if err != nil {
			part.Close()
			writeError(w, http.StatusInternalServerError, errors.New("创建文件失败"))
			return
		}

		peek := make([]byte, 512)
		peekN, readErr := io.ReadFull(part, peek)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			cleanup()
			part.Close()
			writeError(w, http.StatusBadRequest, errors.New("读取上传文件失败"))
			return
		}
		peek = peek[:peekN]
		if !isAllowedFileSignature(ext, peek) {
			cleanup()
			part.Close()
			writeError(w, http.StatusBadRequest, errors.New("文件内容与扩展名不匹配"))
			return
		}

		n, _ := dst.Write(peek)
		written = int64(n)

		n2, err := io.Copy(dst, part)
		if err != nil {
			cleanup()
			part.Close()
			writeError(w, http.StatusInternalServerError, errors.New("保存文件失败"))
			return
		}
		written += n2
		dst.Close()
		dst = nil
		part.Close()

		if err := s.service.RecordAudit(user.Username, "upload-file", destPath, "success"); err != nil {
			log.Printf("上传审计记录失败: %v", err)
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"path": destPath,
			"name": safeName,
			"size": written,
		})
		return
	}

	writeError(w, http.StatusBadRequest, errors.New("缺少上传文件"))
}

func isAllowedFileSignature(ext string, data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	switch ext {
	case ".zip":
		return bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06")) || bytes.HasPrefix(data, []byte("PK\x07\x08"))
	case ".gz", ".tgz", ".tar.gz":
		return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
	case ".tar":
		return len(data) > 265 && string(data[257:262]) == "ustar"
	case ".iso", ".img":
		return len(data) > 0
	case ".json":
		return trimmed[0] == '{' || trimmed[0] == '['
	case ".yaml", ".yml":
		return !bytes.ContainsAny(trimmed, "\x00")
	default:
		return false
	}
}

func isValidUploadType(t string) bool {
	for _, allowed := range []string{"packages", "marketapps", "components", "images", "attestations"} {
		if t == allowed {
			return true
		}
	}
	return false
}

func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	answer, err := randomCaptcha(4)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errors.New("生成验证码失败"))
		return
	}
	idToken, err := randomCaptcha(12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errors.New("生成验证码失败"))
		return
	}
	id := fmt.Sprintf("cap-%s", idToken)
	expiresAt := time.Now().UTC().Add(time.Minute)
	s.captchaM.Lock()
	for key, item := range s.captchas {
		if time.Now().UTC().After(item.ExpiresAt) {
			delete(s.captchas, key)
		}
	}
	s.captchas[id] = captchaChallenge{Answer: answer, ExpiresAt: expiresAt}
	s.captchaM.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "image": captchaImageData(answer), "expiresAt": expiresAt.Format(time.RFC3339)})
}

func captchaImageData(answer string) string {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="50" viewBox="0 0 160 50"><rect width="160" height="50" rx="8" fill="#eff6ff"/><path d="M5 15 C25 5,45 25,65 10 S100 35,120 20 S155 5,160 10" fill="none" stroke="#93c5fd" stroke-width="1.5" opacity="0.7"/><path d="M8 35 C30 20,50 45,75 25 S100 15,125 30 S145 18,160 25" fill="none" stroke="#bfdbfe" stroke-width="1.5" opacity="0.7"/><text x="40" y="30" transform="rotate(-8 40 30)" text-anchor="middle" font-family="monospace" font-size="22" font-weight="700" fill="#1d4ed8" opacity="0.85">%c</text><text x="80" y="32" transform="rotate(5 80 32)" text-anchor="middle" font-family="monospace" font-size="24" font-weight="700" fill="#1e40af" opacity="0.9">%c</text><text x="120" y="28" transform="rotate(-5 120 28)" text-anchor="middle" font-family="monospace" font-size="22" font-weight="700" fill="#2563eb" opacity="0.85">%c</text><text x="140" y="34" transform="rotate(12 140 34)" text-anchor="middle" font-family="monospace" font-size="20" font-weight="700" fill="#1d4ed8" opacity="0.9">%c</text></svg>`, answer[0], answer[1], answer[2], answer[3])
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func randomCaptcha(length int) (string, error) {
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}

func (s *Server) validateCaptcha(id, answer string) bool {
	s.captchaM.Lock()
	defer s.captchaM.Unlock()
	challenge, ok := s.captchas[id]
	delete(s.captchas, id)
	if !ok || time.Now().UTC().After(challenge.ExpiresAt) {
		return false
	}
	return strings.EqualFold(challenge.Answer, strings.TrimSpace(answer))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var payload domain.LoginRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if os.Getenv("GO_TEST_MODE") == "" {
		if !s.validateCaptcha(payload.CaptchaID, payload.CaptchaAnswer) {
			writeError(w, http.StatusUnauthorized, errors.New("验证码错误或已过期"))
			return
		}
		if !s.checkLoginRateLimit(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, errors.New("登录请求过于频繁，请稍后再试"))
			return
		}
	}
	response, err := s.service.Login(payload)
	if err != nil {
		if errors.Is(err, service.ErrAccountLocked) {
			writeError(w, http.StatusLocked, err)
			return
		}
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	isSecure := isSecureRequest(r)
	http.SetCookie(w, &http.Cookie{
		Name:     "sgx-onebox-token",
		Value:    response.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	_, err := s.requireUser(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	token := readToken(r)
	if token != "" {
		s.service.Logout(token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sgx-onebox-token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	user, err := s.requireUser(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var payload domain.ChangePasswordPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.service.ChangePassword(user.ID, payload); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if _, err := s.requireUser(r); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, s.service.BuildDashboardSummary())
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListUsers())
	case http.MethodPost:
		var payload domain.UserPayload
		if !decodeJSON(w, r, &payload) {
			return
		}
		u, err := s.service.SaveUser(payload, user.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, u)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少用户 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().Users {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("用户未找到"))
	case http.MethodPut:
		actor, err := s.requireRoles(r, domain.RolePlatformAdmin)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.UserPayload
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		u, err := s.service.SaveUser(payload, actor.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, u)
	case http.MethodDelete:
		actor, err := s.requireRoles(r, domain.RolePlatformAdmin)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteUser(id, actor.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().Images)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ImageAsset
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveImage(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleImageBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
		writeAuthError(w, err)
		return
	}
	var payload domain.ImageBuildRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	result, err := s.service.BuildImage(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleImageByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/images/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少镜像 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().Images {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("镜像未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ImageAsset
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveImage(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		for _, item := range s.service.Snapshot().Images {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteImage(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListComponents())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ComponentDefinition
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveComponent(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleComponentByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/components/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id := parts[0]
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少组件 ID"))
		return
	}
	if len(parts) == 2 && parts[1] == "deploy" {
		user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		if err := s.service.DeployComponent(id, user.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeComponentOperationResponse(w, s.service.Snapshot(), id)
		return
	}
	if len(parts) == 2 && parts[1] == "manifest" {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		manifest, err := s.service.GenerateManifest(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"manifest": manifest})
		return
	}
	if len(parts) == 2 && parts[1] == "scale" {
		user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Replicas int `json:"replicas"`
		}
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.ScaleComponent(id, payload.Replicas, user.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeComponentOperationResponse(w, s.service.Snapshot(), id)
		return
	}
	if len(parts) == 2 && parts[1] == "upgrade" {
		user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Version string `json:"version"`
		}
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.UpgradeComponent(id, payload.Version, user.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeComponentOperationResponse(w, s.service.Snapshot(), id)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().Components {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("组件未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ComponentDefinition
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveComponent(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		actor, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteComponent(id, actor.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleBatchDeploy(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req domain.BatchDeployRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("请至少选择一个组件"))
		return
	}
	results := s.service.BatchDeployComponents(req.IDs, user.Username)
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleBatchScale(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var req domain.BatchScaleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("请至少选择一个组件"))
		return
	}
	if req.Replicas < 1 {
		writeError(w, http.StatusBadRequest, errors.New("副本数必须大于 0"))
		return
	}
	results := s.service.BatchScaleComponents(req.IDs, req.Replicas, user.Username)
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleNetworks(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListNetworks())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.NetworkAttachment
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveNetwork(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleNetworkByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/networks/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少网络 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().Networks {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("网络未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.NetworkAttachment
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveNetwork(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteNetwork(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleEnclaves(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListEnclaves())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveProfile
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveEnclave(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleEnclaveByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/enclaves/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少飞地配置 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().Enclaves {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("飞地配置未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveProfile
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveEnclave(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteEnclave(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleRunAttestation(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	records, err := s.service.RunAttestation(user.Username)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleAttestations(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.service.Snapshot().Attestations)
}

func (s *Server) handleAttestationByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/attestations/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少证明记录 ID"))
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "result" {
		user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload domain.AttestationResultPayload
		if !decodeJSON(w, r, &payload) {
			return
		}
		record, err := s.service.SubmitAttestationResult(id, payload, user.Username)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, record := range s.service.Snapshot().Attestations {
			if record.ID == id {
				writeJSON(w, http.StatusOK, record)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("证明记录未找到"))
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleInstallPackages(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().InstallPackages)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.InstallPackage
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveInstallPackage(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleInstallPackageByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/install-packages/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少安装包 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().InstallPackages {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("安装包未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.InstallPackage
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveInstallPackage(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteInstallPackage(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleMarketplaceApps(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListMarketplaceApps())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.MarketplaceApp
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveMarketplaceApp(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleMarketplaceAppByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/marketplace-apps/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id := parts[0]
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少应用市场组件 ID"))
		return
	}
	if len(parts) == 2 && parts[1] == "publish" {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		if err := s.service.PublishMarketplaceApp(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
		return
	}
	if len(parts) == 2 && parts[1] == "unpublish" {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		if err := s.service.UnpublishMarketplaceApp(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "unpublished"})
		return
	}
	if len(parts) == 2 && parts[1] == "versions" {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Version     string `json:"version"`
			PackageName string `json:"packageName"`
		}
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.AddMarketplaceAppVersion(id, payload.Version, payload.PackageName); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "version_added"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().MarketplaceApps {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("应用市场组件未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.MarketplaceApp
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveMarketplaceApp(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteMarketplaceApp(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleProvisioningTaskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/provisioning-tasks/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少自动装机任务 ID"))
		return
	}
	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少自动装机任务 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		if len(parts) != 1 {
			writeError(w, http.StatusNotFound, errors.New("自动装机任务接口不存在"))
			return
		}
		for _, task := range s.service.Snapshot().ProvisioningTasks {
			if task.ID == id {
				writeJSON(w, http.StatusOK, task)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("自动装机任务未找到"))
	case http.MethodPost:
		user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if len(parts) != 2 {
			writeError(w, http.StatusNotFound, errors.New("自动装机任务接口不存在"))
			return
		}
		switch parts[1] {
		case "retry":
			if err := s.service.RetryProvisioningTask(id, user.Username); err != nil {
				writeServiceError(w, err)
				return
			}
			writeProvisioningTaskResponse(w, s.service.Snapshot(), id)
		case "cancel":
			if err := s.service.CancelProvisioningTask(id, user.Username); err != nil {
				writeServiceError(w, err)
				return
			}
			writeProvisioningTaskResponse(w, s.service.Snapshot(), id)
		default:
			writeError(w, http.StatusNotFound, errors.New("自动装机任务接口不存在"))
		}
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleProvisioningTasks(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().ProvisioningTasks)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		events := s.service.Snapshot().AuditEvents
		if events == nil {
			events = []domain.AuditEvent{}
		}
		keyword := r.URL.Query().Get("keyword")
		if keyword != "" {
			kw := strings.ToLower(keyword)
			filtered := make([]domain.AuditEvent, 0)
			for _, e := range events {
				if strings.Contains(strings.ToLower(e.Actor), kw) ||
					strings.Contains(strings.ToLower(e.Action), kw) ||
					strings.Contains(strings.ToLower(e.Target), kw) ||
					strings.Contains(strings.ToLower(e.Result), kw) {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}
		writeJSON(w, http.StatusOK, events)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().ClusterNodes)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ClusterNode
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveClusterNode(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeClusterNodeResponse(w, http.StatusCreated, s.service.Snapshot(), payload.ID, payload.Name)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterQuotas(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().ClusterQuotas)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ClusterQuota
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveClusterQuota(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterNodeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/cluster/nodes/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少节点 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().ClusterNodes {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("节点未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ClusterNode
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveClusterNode(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeClusterNodeResponse(w, http.StatusOK, s.service.Snapshot(), id, payload.Name)
	case http.MethodDelete:
		actor, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteClusterNode(id, actor.Username); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterQuotaByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/cluster/quotas/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少配额 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().ClusterQuotas {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("配额未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ClusterQuota
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveClusterQuota(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteClusterQuota(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterUpgrade(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().ClusterUpgrade)
	case http.MethodPost:
		var payload domain.ClusterUpgradeRequest
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.UpgradeCluster(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		upgrade := s.service.Snapshot().ClusterUpgrade
		writeJSON(w, http.StatusOK, operationResponse{Status: upgrade.Status, Result: "accepted", Message: upgrade.Message})
	case http.MethodPut:
		if err := s.service.ResetClusterUpgrade(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		upgrade := s.service.Snapshot().ClusterUpgrade
		writeJSON(w, http.StatusOK, operationResponse{Status: upgrade.Status, Result: "cancelled", Message: upgrade.Message})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleClusterUpgradeDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin); err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var payload domain.ClusterUpgradeRequest
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.service.DownloadClusterUpgrade(payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	upgrade := s.service.Snapshot().ClusterUpgrade
	writeJSON(w, http.StatusOK, operationResponse{Status: upgrade.Status, Result: "accepted", Message: upgrade.Message})
}

func (s *Server) handleAlertThreshold(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.service.Snapshot().AlertThreshold)
		return
	}
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.AlertThresholdConfig
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveAlertThreshold(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	writeMethodNotAllowed(w)
}

func (s *Server) handleEnclaveKeys(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().EnclaveKeys)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveKeyMaterial
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveEnclaveKey(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleEnclaveKeyByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/enclave-keys/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少密钥 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().EnclaveKeys {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("密钥未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveKeyMaterial
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveEnclaveKey(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteEnclaveKey(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleEnclaveResources(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().EnclaveResources)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveResource
		if !decodeJSON(w, r, &payload) {
			return
		}
		saved, err := s.service.SaveEnclaveResource(payload)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleEnclaveResourceByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/enclave-resources/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少可信资源 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().EnclaveResources {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("可信资源未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.EnclaveResource
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		saved, err := s.service.SaveEnclaveResource(payload)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteEnclaveResource(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleRunEnclaveInspection(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	inspections, err := s.service.RunEnclaveInspection()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inspections)
}

func (s *Server) handleEnclaveInspections(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.service.Snapshot().EnclaveInspections)
}

func (s *Server) handleEnclaveInspectionByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/enclave-inspections/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少巡检 ID"))
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	for _, item := range s.service.Snapshot().EnclaveInspections {
		if item.ID == id {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("巡检记录未找到"))
}

func (s *Server) handleSecurityPolicies(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().SecurityPolicies)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.SecurityPolicyRule
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveSecurityPolicyRule(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleSecurityPolicyByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/security-policies/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少策略规则 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().SecurityPolicies {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("策略规则未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.SecurityPolicyRule
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveSecurityPolicyRule(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteSecurityPolicyRule(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleComplianceTasks(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().ComplianceTasks)
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ComplianceTask
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveComplianceTask(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleComplianceTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/compliance-tasks/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少整改任务 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().ComplianceTasks {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("整改任务未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.ComplianceTask
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveComplianceTask(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteComplianceTask(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.Snapshot().SystemSettings)
	case http.MethodPost:
		var payload domain.SystemSetting
		if !decodeJSON(w, r, &payload) {
			return
		}
		setting, err := s.service.SaveSystemSetting(payload, user.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, setting)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleSystemSettingByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/system-settings/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少系统设置 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().SystemSettings {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("系统设置未找到"))
	case http.MethodPut:
		actor, err := s.requireRoles(r, domain.RolePlatformAdmin)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.SystemSetting
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		setting, err := s.service.SaveSystemSetting(payload, actor.Username)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, setting)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteSystemSetting(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleRunCompliance(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	report, err := s.service.RunCompliance(user.Username)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleComplianceExport(w http.ResponseWriter, r *http.Request) {
	user, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	reportID := r.URL.Query().Get("id")
	format := r.URL.Query().Get("format")
	if reportID == "" || format == "" {
		writeError(w, http.StatusBadRequest, errors.New("id and format query parameters required"))
		return
	}
	if format != "csv" && format != "html" {
		writeError(w, http.StatusBadRequest, errors.New("format must be csv or html"))
		return
	}
	data, contentType, filename, err := s.service.ExportComplianceReport(user.Username, reportID, format)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (s *Server) handleTopoLinks(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListTopoLinks())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.TopologyLink
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveTopoLink(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleTopoLinkByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/topo/links/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少拓扑连线 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().TopoLinks {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("拓扑连线未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.TopologyLink
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveTopoLink(payload); err != nil {
			writeServiceError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().TopoLinks {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteTopoLink(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleTopoEgress(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListTopoEgress())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.TopologyNode
		if !decodeJSON(w, r, &payload) {
			return
		}
		if err := s.service.SaveTopoEgress(payload); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeSavedEntityResponse(w, http.StatusCreated, s.service.Snapshot(), payload)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleTopoEgressByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/topo/egress/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("缺少拓扑出口节点 ID"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().TopoEgress {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("拓扑出口节点未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		var payload domain.TopologyNode
		if !decodeJSON(w, r, &payload) {
			return
		}
		payload.ID = id
		if err := s.service.SaveTopoEgress(payload); err != nil {
			writeServiceError(w, err)
			return
		}
		for _, item := range s.service.Snapshot().TopoEgress {
			if item.ID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeleteTopoEgress(id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleK3sVersions(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireUser(r); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "stable"
	}

	if s.k3sCache == nil {
		s.k3sCache = &struct {
			mu    sync.RWMutex
			items map[string]struct {
				versions []string
				expires  time.Time
			}
		}{items: map[string]struct {
			versions []string
			expires  time.Time
		}{}}
	}

	s.k3sCache.mu.RLock()
	entry, ok := s.k3sCache.items[channel]
	s.k3sCache.mu.RUnlock()
	if ok && time.Now().UTC().Before(entry.expires) {
		writeJSON(w, http.StatusOK, entry.versions)
		return
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get("https://api.github.com/repos/k3s-io/k3s/releases?per_page=10")
	if err != nil {
		writeJSON(w, http.StatusOK, fallbackK3sVersions(channel))
		return
	}
	defer resp.Body.Close()

	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		writeJSON(w, http.StatusOK, fallbackK3sVersions(channel))
		return
	}

	var versions []string
	for _, rel := range releases {
		if channel == "stable" && rel.Prerelease {
			continue
		}
		versions = append(versions, rel.TagName)
	}
	if len(versions) == 0 {
		versions = fallbackK3sVersions(channel)
	}

	s.k3sCache.mu.Lock()
	s.k3sCache.items[channel] = struct {
		versions []string
		expires  time.Time
	}{versions: versions, expires: time.Now().UTC().Add(5 * time.Minute)}
	s.k3sCache.mu.Unlock()

	writeJSON(w, http.StatusOK, versions)
}

func fallbackK3sVersions(channel string) []string {
	if channel == "testing" || channel == "latest" {
		return []string{"v1.32.5+k3s1", "v1.31.9+k3s1", "v1.30.13+k3s1"}
	}
	return []string{"v1.31.9+k3s1", "v1.30.13+k3s1", "v1.29.15+k3s1"}
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
		writeAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.service.ListPlugins())
	case http.MethodPost:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		var plugin domain.PluginDefinition
		if !decodeJSON(w, r, &plugin) {
			return
		}
		if strings.TrimSpace(plugin.ID) == "" {
			plugin.ID = "plugin-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
		}
		if err := s.service.SavePlugin(plugin); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, plugin)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handlePluginByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("插件 ID 不能为空"))
		return
	}
	parts := strings.SplitN(id, "/", 2)
	pluginID := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "enable":
			if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
				writeAuthError(w, err)
				return
			}
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w)
				return
			}
			if err := s.service.EnablePlugin(pluginID); err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
			return
		case "disable":
			if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
				writeAuthError(w, err)
				return
			}
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w)
				return
			}
			if err := s.service.DisablePlugin(pluginID); err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
			return
		}
	}
	switch r.Method {
	case http.MethodGet:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin, domain.RoleOperator, domain.RoleAuditor); err != nil {
			writeAuthError(w, err)
			return
		}
		for _, plugin := range s.service.Snapshot().Plugins {
			if plugin.ID == pluginID {
				writeJSON(w, http.StatusOK, plugin)
				return
			}
		}
		writeError(w, http.StatusNotFound, errors.New("插件未找到"))
	case http.MethodPut:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		var plugin domain.PluginDefinition
		if !decodeJSON(w, r, &plugin) {
			return
		}
		plugin.ID = pluginID
		if err := s.service.SavePlugin(plugin); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, plugin)
	case http.MethodDelete:
		if _, err := s.requireRoles(r, domain.RolePlatformAdmin, domain.RoleSecurityAdmin); err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.service.DeletePlugin(pluginID); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) requireUser(r *http.Request) (domain.UserView, error) {
	if isUnsafeMethod(r.Method) && usesCookieToken(r) {
		if !sameOriginRequest(r) {
			return domain.UserView{}, errors.New("请求来源校验失败")
		}
	}
	token := readToken(r)
	if token == "" {
		return domain.UserView{}, errors.New("缺少登录凭证")
	}
	return s.service.CurrentUser(token)
}

func usesCookieToken(r *http.Request) bool {
	_, err := r.Cookie("sgx-onebox-token")
	return err == nil && bearerToken(r) == ""
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		referer := strings.TrimSpace(r.Header.Get("Referer"))
		if referer == "" {
			if os.Getenv("GO_TEST_MODE") != "" {
				return true
			}
			log.Printf("[SECURITY] request missing Origin and Referer headers from %s", r.RemoteAddr)
			return false
		}
		return isTrustedOriginReferrer(referer)
	}
	return isTrustedOriginReferrer(origin)
}

func isTrustedOriginReferrer(rawURL string) bool {
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if strings.HasSuffix(host, ".monkeycode-ai.online") {
		return true
	}
	return false
}

func (s *Server) requireRoles(r *http.Request, roles ...domain.Role) (domain.UserView, error) {
	user, err := s.requireUser(r)
	if err != nil {
		return domain.UserView{}, err
	}
	for _, role := range roles {
		if user.Role == role {
			return user, nil
		}
	}
	return domain.UserView{}, errForbidden
}

func readToken(r *http.Request) string {
	if cookie, err := r.Cookie("sgx-onebox-token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return bearerToken(r)
}

func bearerToken(r *http.Request) string {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		return false
	}
	if proto == "https" {
		host := r.Host
		if colon := strings.LastIndex(host, ":"); colon >= 0 {
			host = host[:colon]
		}
		if isTrustedOriginReferrer(host) {
			return true
		}
	}
	return false
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("CORS_ORIGIN")
		if origin == "" {
			if os.Getenv("GO_TEST_MODE") != "" {
				origin = "*"
			} else {
				origin = "https://localhost"
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if origin != "*" {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'")
		if isSecureRequest(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if os.Getenv("GO_TEST_MODE") != "" {
			w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
			w.Header().Set("Cross-Origin-Opener-Policy", "unsafe-none")
		} else {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeJSON[T any](w http.ResponseWriter, r *http.Request, payload *T) bool {
	defer r.Body.Close()
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, errors.New("请求 Content-Type 必须为 application/json"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("请求体格式无效"))
		return false
	}
	return true
}

type operationResponse struct {
	Status  string `json:"status"`
	Result  string `json:"result"`
	Message string `json:"message"`
}

func writeComponentOperationResponse(w http.ResponseWriter, snapshot store.Snapshot, id string) {
	for _, component := range snapshot.Components {
		if component.ID != id {
			continue
		}
		result := "success"
		message := "操作已完成"
		if component.Status == domain.ComponentDeploying {
			result = "pending_executor"
			message = "请求已登记，等待真实执行器回写结果"
		}
		writeJSON(w, http.StatusOK, operationResponse{Status: string(component.Status), Result: result, Message: message})
		return
	}
	writeError(w, http.StatusNotFound, errors.New("组件未找到"))
}

func writeProvisioningTaskResponse(w http.ResponseWriter, snapshot store.Snapshot, id string) {
	for _, task := range snapshot.ProvisioningTasks {
		if task.ID == id {
			writeJSON(w, http.StatusOK, task)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("自动装机任务未找到"))
}

func writeClusterNodeResponse(w http.ResponseWriter, code int, snapshot store.Snapshot, id string, name string) {
	for _, item := range snapshot.ClusterNodes {
		if item.ID == id || id == "" && item.Name == name {
			writeJSON(w, code, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("节点未找到"))
}

func writeSavedEntityResponse(w http.ResponseWriter, code int, snapshot store.Snapshot, payload any) {
	switch item := payload.(type) {
	case domain.ImageAsset:
		for _, saved := range snapshot.Images {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.Registry == item.Registry && saved.Repository == item.Repository && saved.Tag == item.Tag {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.ComponentDefinition:
		for _, saved := range snapshot.Components {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.Image == item.Image && saved.Version == item.Version {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.MarketplaceApp:
		for _, saved := range snapshot.MarketplaceApps {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.PackageName == item.PackageName && saved.CurrentVersion == item.CurrentVersion {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.ClusterQuota:
		for _, saved := range snapshot.ClusterQuotas {
			if saved.ID == item.ID || item.ID == "" && saved.Scope == item.Scope {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.NetworkAttachment:
		for _, saved := range snapshot.Networks {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.ParentNIC == item.ParentNIC && saved.VLANID == item.VLANID {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.EnclaveProfile:
		for _, saved := range snapshot.Enclaves {
			if saved.ID == item.ID || item.ID == "" && saved.ComponentID == item.ComponentID {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.InstallPackage:
		for _, saved := range snapshot.InstallPackages {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.Version == item.Version {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.EnclaveResource:
		for _, saved := range snapshot.EnclaveResources {
			if saved.ID == item.ID || item.ID == "" && saved.NodeID == item.NodeID {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.EnclaveKeyMaterial:
		for _, saved := range snapshot.EnclaveKeys {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.ComponentID == item.ComponentID {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.ComplianceTask:
		for _, saved := range snapshot.ComplianceTasks {
			if saved.ID == item.ID || item.ID == "" && saved.ControlID == item.ControlID && saved.Owner == item.Owner {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.SecurityPolicyRule:
		for _, saved := range snapshot.SecurityPolicies {
			if saved.ID == item.ID || item.ID == "" && saved.Name == item.Name && saved.Category == item.Category && saved.Scope == item.Scope {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.TopologyLink:
		for _, saved := range snapshot.TopoLinks {
			if saved.ID == item.ID || item.ID == "" && saved.Source == item.Source && saved.Target == item.Target && saved.Kind == item.Kind {
				writeJSON(w, code, saved)
				return
			}
		}
	case domain.TopologyNode:
		for _, saved := range snapshot.TopoEgress {
			if saved.ID == item.ID || item.ID == "" && saved.Kind == item.Kind && saved.RefID == item.RefID && saved.Label == item.Label {
				writeJSON(w, code, saved)
				return
			}
		}
	}
	writeJSON(w, code, payload)
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("writeJSON: 响应编码失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}

func writeAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, errForbidden) {
		writeError(w, http.StatusForbidden, err)
		return
	}
	writeError(w, http.StatusUnauthorized, err)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errors.New("请求方法不支持"))
}

func clientIP(r *http.Request) string {
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	if isTrustedOriginReferrer(r.Host) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	return host
}

func (s *Server) checkLoginRateLimit(ip string) bool {
	s.rateLimitM.Lock()
	defer s.rateLimitM.Unlock()
	now := time.Now().UTC()
	if now.After(s.lastRateLimitCleanup.Add(5 * time.Minute)) {
		for k, v := range s.rateLimits {
			if now.After(v.windowStart.Add(time.Minute)) {
				delete(s.rateLimits, k)
			}
		}
		s.lastRateLimitCleanup = now
	}
	entry, exists := s.rateLimits[ip]
	if !exists || now.After(entry.windowStart.Add(time.Minute)) {
		s.rateLimits[ip] = &rateLimitEntry{count: 1, windowStart: now}
		return true
	}
	entry.count++
	if entry.count > 10 {
		return false
	}
	return true
}
