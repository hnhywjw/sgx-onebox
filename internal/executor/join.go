package executor

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"golang.org/x/crypto/ssh"
)

func tryJoinNode(client *ssh.Client, node *domain.ClusterNode, timeout time.Duration) bool {
	if node.JoinStatus != "credential_ready" && node.JoinStatus != "join_command_ready" {
		return false
	}
	if strings.TrimSpace(node.JoinCommand) == "" {
		return false
	}

	now := time.Now().UTC().Format(time.RFC3339)
	node.LastJoinAttemptAt = now

	cmd := node.JoinCommand
	if strings.Contains(cmd, "${K3S_TOKEN}") {
		token := strings.TrimSpace(os.Getenv("K3S_TOKEN"))
		if token == "" {
			node.JoinStatus = "credential_ready"
			node.LastJoinMessage = "缺少 K3S_TOKEN，等待现场提供真实 join token"
			return false
		}
		cmd = strings.ReplaceAll(cmd, "${K3S_TOKEN}", shellQuote(token))
	}
	if !strings.HasPrefix(strings.TrimSpace(cmd), "k3s ") {
		node.JoinStatus = "failed"
		node.LastJoinMessage = "JoinCommand 必须以 'k3s' 开头"
		return false
	}
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "||") || strings.Contains(cmd, ";") || strings.Contains(cmd, "`") || strings.Contains(cmd, "$(") {
		node.JoinStatus = "failed"
		node.LastJoinMessage = "JoinCommand 包含不允许的 shell 控制字符"
		return false
	}
	output, err := sshExec(client, cmd, timeout)
	if err != nil {
		node.JoinStatus = "failed"
		node.LastJoinMessage = fmt.Sprintf("SSH 执行 join 命令失败: %v", err)
		logWarn(fmt.Sprintf("node %s join failed: %v", node.Name, err))
		return false
	}

	node.JoinStatus = "active"
	node.JoinedAt = now
	node.LastJoinMessage = fmt.Sprintf("节点已通过 SSH 加入集群 (输出: %s)", redactProvisioningEvidence(strings.TrimSpace(output)))
	logDebug(fmt.Sprintf("node %s joined successfully", node.Name))
	return true
}
