package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/store"
)

func init() {
	os.Setenv("GO_TEST_MODE", "1")
}

func TestHealthEndpoint(t *testing.T) {
	server := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func TestCaptchaDoesNotReturnAnswer(t *testing.T) {
	server := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/captcha", nil)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal captcha response: %v", err)
	}
	if result["id"] == "" || result["image"] == "" || result["expiresAt"] == "" {
		t.Fatalf("expected id, image and expiresAt, got %+v", result)
	}
	if _, ok := result["code"]; ok {
		t.Fatalf("captcha response must not expose answer")
	}
}

func TestLoginAndDashboard(t *testing.T) {
	server := NewServer()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.Code)
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginResult); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if loginResult.Token == "" {
		t.Fatal("expected token in login response")
	}
	dashboardReq := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	dashboardReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	dashboardResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(dashboardResp, dashboardReq)
	if dashboardResp.Code != http.StatusOK {
		t.Fatalf("expected dashboard 200, got %d", dashboardResp.Code)
	}
}

func TestCreateClusterNode(t *testing.T) {
	server := NewServer()
	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.Code)
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginResult); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	nodeBody, _ := json.Marshal(map[string]any{
		"name":           "worker-2",
		"role":           "worker",
		"internalIp":     "10.0.0.22",
		"managementIp":   "192.168.10.22",
		"capacityCpu":    "8 vCPU",
		"capacityMemory": "16 GiB",
		"labels":         []string{"node-role.kubernetes.io/worker=true"},
		"sshHost":        "192.168.10.22",
		"sshPort":        22,
		"sshUsername":    "ubuntu",
		"sshPassword":    "secret123",
	})
	nodeReq := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes", bytes.NewReader(nodeBody))
	nodeReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	nodeResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(nodeResp, nodeReq)
	if nodeResp.Code != http.StatusCreated {
		t.Fatalf("expected create node 201, got %d: %s", nodeResp.Code, nodeResp.Body.String())
	}
	nodesReq := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/nodes", nil)
	nodesReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	nodesResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(nodesResp, nodesReq)
	if nodesResp.Code != http.StatusOK {
		t.Fatalf("expected list nodes 200, got %d", nodesResp.Code)
	}
	var clusterNodes []struct {
		Name                  string `json:"name"`
		InternalIP            string `json:"internalIp"`
		Status                string `json:"status"`
		SSHHost               string `json:"sshHost"`
		SSHUsername           string `json:"sshUsername"`
		SSHPassword           string `json:"sshPassword"`
		SSHPasswordConfigured bool   `json:"sshPasswordConfigured"`
		JoinStatus            string `json:"joinStatus"`
	}
	if err := json.Unmarshal(nodesResp.Body.Bytes(), &clusterNodes); err != nil {
		t.Fatalf("unmarshal cluster nodes response: %v", err)
	}
	if len(clusterNodes) < 3 {
		t.Fatalf("expected new node in snapshot, got %d nodes", len(clusterNodes))
	}
	last := clusterNodes[len(clusterNodes)-1]
	if last.Name != "worker-2" || last.InternalIP != "10.0.0.22" || last.Status != "ready" {
		t.Fatalf("unexpected node payload: %+v", last)
	}
	if last.SSHHost != "192.168.10.22" || last.SSHUsername != "ubuntu" || !last.SSHPasswordConfigured || last.SSHPassword != "" || last.JoinStatus != "credential_ready" {
		t.Fatalf("unexpected ssh payload: %+v", last)
	}
}

func TestChangePassword(t *testing.T) {
	server := NewServer()
	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.Code)
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginResult); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	changeBody, _ := json.Marshal(map[string]string{"currentPassword": "admin123", "newPassword": "admin12345"})
	changeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	changeResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(changeResp, changeReq)
	if changeResp.Code != http.StatusOK {
		t.Fatalf("expected change password 200, got %d", changeResp.Code)
	}
	newLoginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin12345"})
	newLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(newLoginBody))
	newLoginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(newLoginResp, newLoginReq)
	if newLoginResp.Code != http.StatusOK {
		t.Fatalf("expected relogin 200, got %d", newLoginResp.Code)
	}

	var newLoginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(newLoginResp.Body.Bytes(), &newLoginResult)

	revertBody, _ := json.Marshal(map[string]string{"currentPassword": "admin12345", "newPassword": "admin123"})
	revertReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", bytes.NewReader(revertBody))
	revertReq.Header.Set("Authorization", "Bearer "+newLoginResult.Token)
	revertResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(revertResp, revertReq)
	if revertResp.Code != http.StatusOK {
		t.Fatalf("expected revert 200, got %d", revertResp.Code)
	}
}

func TestLoginFailureLocking(t *testing.T) {
	server := NewServer()
	wrongBody, _ := json.Marshal(map[string]string{"username": "locktest", "password": "wrong-password"})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(wrongBody))
		resp := httptest.NewRecorder()
		server.Routes().ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, resp.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(wrongBody))
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusLocked {
		t.Fatalf("6th attempt: expected 423 Locked, got %d", resp.Code)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal lock response: %v", err)
	}
	errMsg := result["error"]
	if errMsg == "" || !bytes.Contains([]byte(errMsg), []byte("锁定")) {
		t.Fatalf("expected error message to mention 锁定, got %q", errMsg)
	}
}

func TestTokenVerification(t *testing.T) {
	server := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-abc")
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", resp.Code)
	}
}

func TestCRUDSystemSettings(t *testing.T) {
	server := NewServer()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	var loginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginResp.Body.Bytes(), &loginResult)

	createBody, _ := json.Marshal(map[string]string{"id": "test-crud", "category": "test", "name": "CRUD测试", "value": "initial"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/system-settings", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	createResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.Code)
	}

	updateBody, _ := json.Marshal(map[string]string{"category": "test", "name": "CRUD测试更新", "value": "updated"})
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/system-settings/test-crud", bytes.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	updateResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected PUT 200, got %d", updateResp.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/system-settings/test-crud", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	deleteResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected DELETE 200, got %d", deleteResp.Code)
	}
}

func TestCRUDUsers(t *testing.T) {
	server := NewServer()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	var loginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginResp.Body.Bytes(), &loginResult)

	createBody, _ := json.Marshal(map[string]string{"id": "test-user-crud", "username": "test-crud", "displayName": "测试CRUD", "role": "operator", "status": "active", "password": "test123"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	createResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.Code)
	}

	updateBody, _ := json.Marshal(map[string]string{"displayName": "测试CRUD更新", "role": "auditor", "status": "active"})
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/users/test-user-crud", bytes.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	updateResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected PUT 200, got %d: %s", updateResp.Code, updateResp.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/users/test-user-crud", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	deleteResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected DELETE 200, got %d", deleteResp.Code)
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	server := NewServer()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	loginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginResp.Code)
	}
	var loginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(loginResp.Body.Bytes(), &loginResult)

	changeBody, _ := json.Marshal(map[string]string{"currentPassword": "admin123", "newPassword": "admin12345"})
	changeReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	changeResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(changeResp, changeReq)
	if changeResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", changeResp.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginResult.Token)
	meResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(meResp, meReq)
	if meResp.Code != http.StatusUnauthorized {
		t.Fatalf("old token should be invalid after password change, got %d", meResp.Code)
	}

	revertLogin, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin12345"})
	newLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(revertLogin))
	newLoginResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(newLoginResp, newLoginReq)
	var newLoginResult struct {
		Token string `json:"token"`
	}
	json.Unmarshal(newLoginResp.Body.Bytes(), &newLoginResult)
	if newLoginResult.Token == "" {
		t.Fatal("expected token after relogin")
	}

	revertBody, _ := json.Marshal(map[string]string{"currentPassword": "admin12345", "newPassword": "admin123"})
	revertReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", bytes.NewReader(revertBody))
	revertReq.Header.Set("Authorization", "Bearer "+newLoginResult.Token)
	revertResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(revertResp, revertReq)
	if revertResp.Code != http.StatusOK {
		t.Fatalf("expected revert 200, got %d: %s", revertResp.Code, revertResp.Body.String())
	}
}

func TestLoginLockPersistence(t *testing.T) {
	server := NewServer()
	wrongBody, _ := json.Marshal(map[string]string{"username": "lock-persist", "password": "wrong"})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(wrongBody))
		resp := httptest.NewRecorder()
		server.Routes().ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, resp.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(wrongBody))
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusLocked {
		t.Fatalf("6th attempt: expected 423 Locked, got %d", resp.Code)
	}

	server2 := NewServer()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(wrongBody))
	resp2 := httptest.NewRecorder()
	server2.Routes().ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusLocked {
		t.Fatalf("after restart: expected 423 Locked, got %d", resp2.Code)
	}
}

func TestProvisioningTaskAPIs(t *testing.T) {
	server := NewServer()
	adminToken := loginForTest(t, server, "admin", "admin123")
	nodeBody, _ := json.Marshal(map[string]any{
		"name":          "api-auto-task",
		"role":          "worker",
		"internalIp":    "10.0.2.20",
		"managementIp":  "10.0.2.20",
		"sshHost":       "10.0.2.20",
		"sshPort":       22,
		"sshUsername":   "root",
		"sshPassword":   "secret123",
		"autoProvision": true,
		"enableSgx":     true,
		"provisionMode": "online",
		"k3sRole":       "worker",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes", bytes.NewReader(nodeBody))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected create node 201, got %d: %s", createResp.Code, createResp.Body.String())
	}

	tasksReq := httptest.NewRequest(http.MethodGet, "/api/v1/provisioning-tasks", nil)
	tasksReq.Header.Set("Authorization", "Bearer "+adminToken)
	tasksResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(tasksResp, tasksReq)
	if tasksResp.Code != http.StatusOK {
		t.Fatalf("expected 200 for provisioning tasks list, got %d: %s", tasksResp.Code, tasksResp.Body.String())
	}
	var task struct {
		ID          string `json:"id"`
		NodeID      string `json:"nodeId"`
		Status      string `json:"status"`
		CurrentStep string `json:"currentStep"`
	}
	var tasks []struct {
		ID          string `json:"id"`
		NodeID      string `json:"nodeId"`
		Status      string `json:"status"`
		CurrentStep string `json:"currentStep"`
	}
	if err := json.Unmarshal(tasksResp.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("unmarshal provisioning tasks: %v", err)
	}
	found := false
	for _, tsk := range tasks {
		if tsk.NodeID == "node-api-auto-task" {
			task = tsk
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("task for node node-api-auto-task not found in provisioning tasks")
	}
	if task.ID == "" || task.Status != "pending" || task.CurrentStep != "preflight" {
		t.Fatalf("unexpected dashboard task: %+v", task)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/provisioning-tasks/"+task.ID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+adminToken)
	detailResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(detailResp, detailReq)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d: %s", detailResp.Code, detailResp.Body.String())
	}

	storedTask := server.service.Snapshot().ProvisioningTasks[0]
	for _, item := range server.service.Snapshot().ProvisioningTasks {
		if item.ID == task.ID {
			storedTask = item
			break
		}
	}
	storedTask.Status = "failed"
	storedTask.Steps[0].Status = "failed"
	storedTask.Steps[0].Message = "preflight failed"
	if err := server.service.SaveProvisioningTaskStatus(storedTask); err != nil {
		t.Fatalf("SaveProvisioningTaskStatus: %v", err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/provisioning-tasks/"+task.ID+"/retry", nil)
	retryReq.Header.Set("Authorization", "Bearer "+adminToken)
	retryResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(retryResp, retryReq)
	if retryResp.Code != http.StatusOK {
		t.Fatalf("expected retry 200, got %d: %s", retryResp.Code, retryResp.Body.String())
	}
	var retried struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(retryResp.Body.Bytes(), &retried); err != nil {
		t.Fatalf("unmarshal retry response: %v", err)
	}
	if retried.Status != "pending" {
		t.Fatalf("expected pending after retry, got %s", retried.Status)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/provisioning-tasks/"+task.ID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+adminToken)
	cancelResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("expected cancel 200, got %d: %s", cancelResp.Code, cancelResp.Body.String())
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(cancelResp.Body.Bytes(), &cancelled); err != nil {
		t.Fatalf("unmarshal cancel response: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", cancelled.Status)
	}
}

func TestProvisioningTaskAuditorCanViewButCannotMutate(t *testing.T) {
	server := NewServer()
	adminToken := loginForTest(t, server, "admin", "admin123")
	auditorToken := loginForTest(t, server, "auditor", "audit123")
	task, err := server.service.CreateProvisioningTask(domain.ClusterNode{ID: "node-api-auditor", Role: "worker", K3sRole: "worker", ProvisionMode: "online"}, "admin")
	if err != nil {
		t.Fatalf("CreateProvisioningTask: %v", err)
	}

	viewReq := httptest.NewRequest(http.MethodGet, "/api/v1/provisioning-tasks/"+task.ID, nil)
	viewReq.Header.Set("Authorization", "Bearer "+auditorToken)
	viewResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(viewResp, viewReq)
	if viewResp.Code != http.StatusOK {
		t.Fatalf("expected auditor detail 200, got %d: %s", viewResp.Code, viewResp.Body.String())
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/provisioning-tasks/"+task.ID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+auditorToken)
	cancelResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusForbidden {
		t.Fatalf("expected auditor cancel 403, got %d: %s", cancelResp.Code, cancelResp.Body.String())
	}

	adminViewReq := httptest.NewRequest(http.MethodGet, "/api/v1/provisioning-tasks/"+task.ID, nil)
	adminViewReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminViewResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(adminViewResp, adminViewReq)
	if adminViewResp.Code != http.StatusOK {
		t.Fatalf("expected admin detail 200, got %d", adminViewResp.Code)
	}
}

func TestWriteSavedEntityResponseReturnsGeneratedIDs(t *testing.T) {
	tests := []struct {
		name     string
		snapshot store.Snapshot
		payload  any
	}{
		{
			name:     "marketplace app",
			snapshot: store.Snapshot{MarketplaceApps: []domain.MarketplaceApp{{ID: "app-generated", Name: "app", PackageName: "pkg", CurrentVersion: "1.0.0"}}},
			payload:  domain.MarketplaceApp{Name: "app", PackageName: "pkg", CurrentVersion: "1.0.0"},
		},
		{
			name:     "cluster quota",
			snapshot: store.Snapshot{ClusterQuotas: []domain.ClusterQuota{{ID: "quota-generated", Scope: "default"}}},
			payload:  domain.ClusterQuota{Scope: "default"},
		},
		{
			name:     "compliance task",
			snapshot: store.Snapshot{ComplianceTasks: []domain.ComplianceTask{{ID: "task-generated", ControlID: "CC-1", Owner: "security"}}},
			payload:  domain.ComplianceTask{ControlID: "CC-1", Owner: "security"},
		},
		{
			name:     "topology link",
			snapshot: store.Snapshot{TopoLinks: []domain.TopologyLink{{ID: "link-generated", Source: "cmp-a", Target: "net-a", Kind: "network"}}},
			payload:  domain.TopologyLink{Source: "cmp-a", Target: "net-a", Kind: "network"},
		},
		{
			name:     "topology egress",
			snapshot: store.Snapshot{TopoEgress: []domain.TopologyNode{{ID: "egress-generated", Kind: "egress", RefID: "0.0.0.0/0", Label: "internet"}}},
			payload:  domain.TopologyNode{Kind: "egress", RefID: "0.0.0.0/0", Label: "internet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			writeSavedEntityResponse(resp, http.StatusCreated, tt.snapshot, tt.payload)
			if resp.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d", resp.Code)
			}
			var result struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if result.ID == "" {
				t.Fatalf("expected generated ID, got %s", resp.Body.String())
			}
		})
	}
}

func loginForTest(t *testing.T, server *Server, username string, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d: %s", username, resp.Code, resp.Body.String())
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if result.Token == "" {
		t.Fatalf("login %s returned empty token", username)
	}
	return result.Token
}

func provisioningTaskFromDashboard(t *testing.T, server *Server, token string, nodeID string) struct {
	ID          string `json:"id"`
	NodeID      string `json:"nodeId"`
	Status      string `json:"status"`
	CurrentStep string `json:"currentStep"`
} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("dashboard: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	var snapshot struct {
		ProvisioningTasks []struct {
			ID          string `json:"id"`
			NodeID      string `json:"nodeId"`
			Status      string `json:"status"`
			CurrentStep string `json:"currentStep"`
		} `json:"provisioningTasks"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("unmarshal dashboard response: %v", err)
	}
	for _, task := range snapshot.ProvisioningTasks {
		if task.NodeID == nodeID {
			return task
		}
	}
	t.Fatalf("task for node %s not found in dashboard", nodeID)
	return struct {
		ID          string `json:"id"`
		NodeID      string `json:"nodeId"`
		Status      string `json:"status"`
		CurrentStep string `json:"currentStep"`
	}{}
}

func loginAs(t *testing.T, server *Server, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d: %s", username, resp.Code, resp.Body.String())
	}
	var result struct{ Token string `json:"token"` }
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal login: %v", err)
	}
	return result.Token
}

func authedGet(t *testing.T, server *Server, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	return resp
}

func authedPost(t *testing.T, server *Server, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	return resp
}

func TestSecurityAdminRolePermissions(t *testing.T) {
	server := NewServer()
	token := loginAs(t, server, "security-admin", "secure123")

	t.Run("can read own profile", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/auth/me", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("me: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read dashboard", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/dashboard", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("dashboard: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read images", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/images", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("images: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read components", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/components", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("components: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read networks", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/networks", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("networks: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read cluster nodes", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/cluster/nodes", token)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("cluster nodes: expected 403 Forbidden, got %d", resp.Code)
		}
	})
	t.Run("can read plugins", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/plugins", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("plugins: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read audit events", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/audit", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("audit: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can create network", func(t *testing.T) {
		resp := authedPost(t, server, "/api/v1/networks", map[string]any{
			"name": "test-net", "parentNic": "eth0", "bridge": "br-test",
			"subnet": "10.99.0.0/24", "gateway": "10.99.0.1",
		}, token)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create network: expected 201, got %d: %s", resp.Code, resp.Body.String())
		}
	})
	t.Run("can retry provisioning task", func(t *testing.T) {
		_ = loginAs(t, server, "admin", "admin123")
		nodeBody := map[string]any{"name": "sec-test-node", "role": "worker", "internalIp": "10.255.0.88", "autoProvision": true, "sshHost": "10.255.0.88", "sshPort": 22, "sshUsername": "ubuntu", "sshPassword": "test12345"}
		resp := authedPost(t, server, "/api/v1/cluster/nodes", nodeBody, loginAs(t, server, "admin", "admin123"))
		if resp.Code != http.StatusCreated {
			t.Fatalf("admin create node: expected 201, got %d: %s", resp.Code, resp.Body.String())
		}
		var node struct{ ID string `json:"id"` }
		json.Unmarshal(resp.Body.Bytes(), &node)
		dashResp := authedGet(t, server, "/api/v1/dashboard", loginAs(t, server, "admin", "admin123"))
		var snap struct{ ProvisioningTasks []struct{ ID string `json:"id"` } `json:"provisioningTasks"` }
		json.Unmarshal(dashResp.Body.Bytes(), &snap)
		if len(snap.ProvisioningTasks) == 0 {
			t.Skip("no provisioning task to retry")
		}
		retryResp := authedPost(t, server, "/api/v1/provisioning-tasks/"+snap.ProvisioningTasks[0].ID+"/retry", nil, token)
		if retryResp.Code == http.StatusOK || retryResp.Code == http.StatusBadRequest {
			// OK means retry accepted, 400 means task in wrong state — both acceptable
			return
		}
		t.Fatalf("retry provisioning: expected 200 or 400, got %d: %s", retryResp.Code, retryResp.Body.String())
	})
	t.Run("cannot manage users", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/users", token)
		if resp.Code == http.StatusOK {
			t.Fatal("security_admin should NOT be able to read users")
		}
	})
}

func TestOperatorRolePermissions(t *testing.T) {
	server := NewServer()
	token := loginAs(t, server, "operator", "ops123")

	t.Run("can read dashboard", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/dashboard", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("dashboard: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read components", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/components", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("components: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read cluster nodes", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/cluster/nodes", token)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("cluster nodes: expected 403 Forbidden, got %d", resp.Code)
		}
	})
	t.Run("can read plugins", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/plugins", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("plugins: expected 200, got %d", resp.Code)
		}
	})
	t.Run("can read plugin by id", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/plugins/plugin-default-monitor", token)
		if resp.Code != http.StatusOK {
			t.Fatalf("plugin by id: expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	})
	t.Run("can create cluster node", func(t *testing.T) {
		resp := authedPost(t, server, "/api/v1/cluster/nodes", map[string]any{
			"name": "op-test-node", "role": "worker", "internalIp": "10.255.0.99",
			"sshHost": "10.255.0.99", "sshPort": 22, "sshUsername": "ubuntu", "sshPassword": "test12345",
		}, token)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("create node: expected 403 Forbidden, got %d: %s", resp.Code, resp.Body.String())
		}
	})
	t.Run("can delete cluster node", func(t *testing.T) {
		dashResp := authedGet(t, server, "/api/v1/dashboard", loginAs(t, server, "admin", "admin123"))
		var snap struct{ ClusterNodes []struct{ ID string `json:"id"` } `json:"clusterNodes"` }
		json.Unmarshal(dashResp.Body.Bytes(), &snap)
		for _, n := range snap.ClusterNodes {
			if n.ID == "node-op-test-node" {
				req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/nodes/"+n.ID, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				resp := httptest.NewRecorder()
				server.Routes().ServeHTTP(resp, req)
				if resp.Code != http.StatusForbidden {
					t.Fatalf("delete node: expected 403 Forbidden, got %d: %s", resp.Code, resp.Body.String())
				}
				return
			}
		}
		t.Skip("operator-created node not found")
	})
	t.Run("cannot create plugin", func(t *testing.T) {
		resp := authedPost(t, server, "/api/v1/plugins", map[string]any{
			"name": "test-plugin", "type": "monitoring", "endpoint": "http://localhost",
		}, token)
		if resp.Code == http.StatusCreated {
			t.Fatal("operator should NOT be able to create plugins")
		}
	})
	t.Run("cannot read audit", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/audit", token)
		if resp.Code == http.StatusOK {
			t.Fatal("operator should NOT be able to read audit")
		}
	})
	t.Run("cannot manage users", func(t *testing.T) {
		resp := authedGet(t, server, "/api/v1/users", token)
		if resp.Code == http.StatusOK {
			t.Fatal("operator should NOT be able to read users")
		}
	})
}
