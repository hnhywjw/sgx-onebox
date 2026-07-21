package executor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/monkeycode-ai/sgx-onebox-platform/internal/domain"
	"github.com/monkeycode-ai/sgx-onebox-platform/internal/security"
)

type StoreAccess interface {
	ListClusterNodes() []domain.ClusterNode
	SaveClusterNodes(nodes []domain.ClusterNode) error
	SaveClusterNodeStatus(node domain.ClusterNode) error
	ListInstallPackages() []domain.InstallPackage
	ListProvisioningTasks() []domain.ProvisioningTask
	SaveProvisioningTaskStatus(task domain.ProvisioningTask) error
}

type Executor struct {
	store                   StoreAccess
	pollInterval            time.Duration
	sshTimeout              time.Duration
	provisioningConcurrency int
	provisioningRunner      provisioningCommandRunner
	consecutive             map[string]int
	nextAttemptAfter        map[string]time.Time
	cancel                  context.CancelFunc
	mu                      sync.Mutex
	stopCh                  chan struct{}
	stopOnce                sync.Once
}

type Config struct {
	Store                   StoreAccess
	PollIntervalSec         int
	SSHTimeoutSec           int
	ProvisioningConcurrency int
}

func New(cfg Config) *Executor {
	poll := time.Duration(cfg.PollIntervalSec) * time.Second
	if poll <= 0 {
		poll = 10 * time.Second
	}
	timeout := time.Duration(cfg.SSHTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	concurrency := cfg.ProvisioningConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	return &Executor{
		store:                   cfg.Store,
		pollInterval:            poll,
		sshTimeout:              timeout,
		provisioningConcurrency: concurrency,
		provisioningRunner:      realProvisioningCommandRunner{timeout: timeout},
		consecutive:             map[string]int{},
		nextAttemptAfter:        map[string]time.Time{},
		stopCh:                  make(chan struct{}),
	}
}

func (e *Executor) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(e.pollInterval)
		defer ticker.Stop()

		logDebug(fmt.Sprintf("executor started, poll interval=%v, ssh timeout=%v", e.pollInterval, e.sshTimeout))

		done := false
		for !done {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[executor] poll cycle panic recovered: %v", r)
					}
				}()
				select {
				case <-ticker.C:
					e.pollOnce()
				case <-e.stopCh:
					logDebug("executor stopped")
					done = true
				case <-ctx.Done():
					logDebug("executor cancelled")
					done = true
				}
			}()
		}
	}()
}

func (e *Executor) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		if e.cancel != nil {
			e.cancel()
		}
	})
}

func (e *Executor) pollOnce() {
	e.pollProvisioningTasks()
	nodes := e.store.ListClusterNodes()

	e.mu.Lock()
	activeIDs := map[string]bool{}
	for _, n := range nodes {
		activeIDs[n.ID] = true
	}
	for id := range e.consecutive {
		if !activeIDs[id] {
			delete(e.consecutive, id)
			delete(e.nextAttemptAfter, id)
		}
	}
	e.mu.Unlock()

	if len(nodes) == 0 {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var wg sync.WaitGroup
	results := make([]nodeResult, len(nodes))

	for i := range nodes {
		e.mu.Lock()
		after, hasBackoff := e.nextAttemptAfter[nodes[i].ID]
		e.mu.Unlock()
		if hasBackoff && time.Now().Before(after) {
			results[i] = nodeResult{node: domain.ClusterNode{ID: nodes[i].ID}, skipped: true}
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[executor] panic in pollOnce worker %d: %v", idx, r)
				}
			}()
			node := nodes[idx]
			result := e.collectAndJoin(node, now)
			results[idx] = result
		}(i)
	}

	wg.Wait()

	for i, r := range results {
		if r.skipped {
			continue
		}
		if r.collected {
			if err := e.store.SaveClusterNodeStatus(r.node); err != nil {
				logWarn(fmt.Sprintf("persist node status failed: %v", err))
			}
			e.mu.Lock()
			e.consecutive[r.node.ID] = 0
			delete(e.nextAttemptAfter, r.node.ID)
			e.mu.Unlock()
		} else {
			e.mu.Lock()
			e.consecutive[r.node.ID]++
			failures := e.consecutive[r.node.ID]
			backoff := time.Duration(1<<min(failures, 6)) * time.Second
			e.nextAttemptAfter[r.node.ID] = time.Now().Add(backoff)
			e.mu.Unlock()
			if failures >= 3 && nodes[i].Status == "ready" {
				updated := nodes[i]
				updated.Status = "unreachable"
				updated.LastJoinMessage = fmt.Sprintf("连续 %d 次 SSH 连接失败", failures)
				if err := e.store.SaveClusterNodeStatus(updated); err != nil {
					logWarn(fmt.Sprintf("persist node unreachable status failed: %v", err))
				}
			}
		}
	}
}

type nodeResult struct {
	node      domain.ClusterNode
	collected bool
	skipped   bool
}

func (e *Executor) collectAndJoin(node domain.ClusterNode, now string) nodeResult {
	result := nodeResult{node: node, collected: false}

	if node.SSHHost == "" || node.SSHUsername == "" || node.SSHPasswordCiphertext == "" {
		return result
	}

	password, err := security.DecryptString(node.SSHPasswordCiphertext)
	if err != nil || password == "" {
		logWarn(fmt.Sprintf("node %s decrypt password failed", node.Name))
		return result
	}
	defer zeroPassword([]byte(password))

	client, err := sshConnect(node.SSHHost, node.SSHPort, node.SSHUsername, []byte(password), node.SSHKnownHostKey, e.sshTimeout)
	if err != nil {
		logWarn(fmt.Sprintf("node %s ssh connect failed: %v", node.Name, err))
		return result
	}
	defer client.Close()

	metrics, err := collectNodeMetrics(client, node, e.sshTimeout)
	if err != nil {
		logWarn(fmt.Sprintf("node %s collect metrics: %v", node.Name, err))
		result.node.LastJoinMessage = "SSH 已连接，但真实节点指标采集失败"
		return result
	}
	applyMetrics(&result.node, metrics, e.pollInterval.Seconds())
	result.collected = true

	result.node.LastHeartbeat = now

	tryJoinNode(client, &result.node, e.sshTimeout*2)

	return result
}

func zeroPassword(pwd []byte) {
	for i := range pwd {
		pwd[i] = 0
	}
}
