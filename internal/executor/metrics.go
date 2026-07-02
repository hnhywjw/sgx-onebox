package executor

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"golang.org/x/crypto/ssh"
)

type nodeMetrics struct {
	CPUUsage    int
	MemoryUsage int
	DiskUsage   int
	RxBytes     int64
	TxBytes     int64
}

func collectNodeMetrics(client *ssh.Client, node domain.ClusterNode, timeout time.Duration) (nodeMetrics, error) {
	var metrics nodeMetrics

	cpuRaw, err := sshExec(client, "top -bn1 | grep 'Cpu(s)' | awk '{print $2+$4}'", timeout)
	if err != nil {
		return metrics, fmt.Errorf("cpu: %w", err)
	}
	cpuVal, parseErr := strconv.ParseFloat(strings.TrimSpace(cpuRaw), 64)
	if parseErr == nil {
		metrics.CPUUsage = int(cpuVal + 0.5)
	}

	memRaw, err := sshExec(client, "free | grep Mem | awk '{printf \"%.1f\", $3/$2*100}'", timeout)
	if err != nil {
		return metrics, fmt.Errorf("mem: %w", err)
	}
	memVal, parseErr := strconv.ParseFloat(strings.TrimSpace(memRaw), 64)
	if parseErr == nil {
		metrics.MemoryUsage = int(memVal + 0.5)
	}

	diskRaw, err := sshExec(client, "df / | tail -1 | awk '{print $5}' | tr -d '%'", timeout)
	if err != nil {
		return metrics, fmt.Errorf("disk: %w", err)
	}
	diskVal, parseErr := strconv.ParseInt(strings.TrimSpace(diskRaw), 10, 64)
	if parseErr == nil {
		metrics.DiskUsage = int(diskVal)
	}

	nicName := node.NicName
	if nicName == "" {
		nicName = "eth0"
	}
	if !isValidLinuxInterface(nicName) {
		return metrics, fmt.Errorf("nic: invalid interface name")
	}

	rxRaw, err := sshExec(client, fmt.Sprintf("cat /sys/class/net/%s/statistics/rx_bytes 2>/dev/null || echo 0", shellQuote(nicName)), timeout)
	if err == nil {
		rxVal, parseErr := strconv.ParseInt(strings.TrimSpace(rxRaw), 10, 64)
		if parseErr == nil {
			metrics.RxBytes = rxVal
		}
	}

	txRaw, err := sshExec(client, fmt.Sprintf("cat /sys/class/net/%s/statistics/tx_bytes 2>/dev/null || echo 0", shellQuote(nicName)), timeout)
	if err == nil {
		txVal, parseErr := strconv.ParseInt(strings.TrimSpace(txRaw), 10, 64)
		if parseErr == nil {
			metrics.TxBytes = txVal
		}
	}

	return metrics, nil
}

func isValidLinuxInterface(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 15 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return !strings.Contains(value, "..")
}

func applyMetrics(node *domain.ClusterNode, m nodeMetrics, intervalSec float64) {
	if m.CPUUsage > 0 {
		node.CPUUsage = m.CPUUsage
	}
	if m.MemoryUsage > 0 {
		node.MemoryUsage = m.MemoryUsage
	}
	if m.DiskUsage > 0 {
		node.DiskUsage = m.DiskUsage
	}
	if m.RxBytes > 0 && intervalSec > 0 && node.RxBytes > 0 {
		node.RxRate = float64(m.RxBytes-node.RxBytes) / intervalSec
	}
	if m.RxBytes > 0 {
		node.RxBytes = m.RxBytes
	}
	if m.TxBytes > 0 && intervalSec > 0 && node.TxBytes > 0 {
		node.TxRate = float64(m.TxBytes-node.TxBytes) / intervalSec
	}
	if m.TxBytes > 0 {
		node.TxBytes = m.TxBytes
	}
}

func logDebug(msg string) {
	log.Printf("[executor] %s", msg)
}

func logWarn(msg string) {
	log.Printf("[executor] WARN: %s", msg)
}
