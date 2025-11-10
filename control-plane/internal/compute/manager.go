package compute

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/linux/projects/server/control-plane/internal/state"
	"github.com/linux/projects/server/control-plane/pkg/types"
)

// Manager manages compute node lifecycle in Kubernetes
type Manager struct {
	k8sClient  kubernetes.Interface
	stateStore state.StoreInterface
	namespace  string
}

// NewManager creates a new compute manager
func NewManager(k8sConfig *rest.Config, stateStore state.StoreInterface, namespace string) (*Manager, error) {
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}

	return &Manager{
		k8sClient:  clientset,
		stateStore: stateStore,
		namespace:  namespace,
	}, nil
}

// CreateComputeNode creates a new MariaDB compute node in Kubernetes
func (m *Manager) CreateComputeNode(projectID string, config types.ComputeConfig) (*types.ComputeNode, error) {
	// Generate compute node ID
	computeID := uuid.New().String()

	// Get project to retrieve config
	project, err := m.stateStore.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	// Use project config if compute config is empty
	if config.PageServerURL == "" {
		config.PageServerURL = project.Config.PageServerURL
	}
	if config.SafekeeperURL == "" {
		config.SafekeeperURL = project.Config.SafekeeperURL
	}
	if config.Image == "" {
		// Default to patched MariaDB image with Page Server support
		// Use environment variable or default to stackblaze image
		config.Image = os.Getenv("MARIADB_PAGESERVER_IMAGE")
		if config.Image == "" {
			config.Image = "stackblaze/mariadb-pageserver:latest" // Custom patched image from stackblaze
		}
	}
	if config.Resources.CPU == "" {
		config.Resources.CPU = "100m" // Reduced for k3s/local development
	}
	if config.Resources.Memory == "" {
		config.Resources.Memory = "256Mi" // Reduced for k3s/local development
	}

	// Create Kubernetes pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("compute-%s", computeID[:8]),
			Namespace: m.namespace,
			Labels: map[string]string{
				"app":        "mariadb-compute",
				"project-id": projectID,
				"compute-id": computeID,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "mariadb",
					Image: config.Image,
					Env: []corev1.EnvVar{
						{Name: "PAGE_SERVER_URL", Value: config.PageServerURL},
						{Name: "SAFEKEEPER_URL", Value: config.SafekeeperURL},
						{Name: "PROJECT_ID", Value: projectID},
						{Name: "COMPUTE_ID", Value: computeID},
						{Name: "MYSQL_ROOT_PASSWORD", Value: "root"},
						{Name: "MYSQL_DATABASE", Value: "test"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(config.Resources.CPU),
							corev1.ResourceMemory: resource.MustParse(config.Resources.Memory),
						},
					},
					Ports: []corev1.ContainerPort{
						{ContainerPort: 3306, Name: "mysql"},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
			Tolerations: []corev1.Toleration{
				{
					Key:      "node.kubernetes.io/disk-pressure",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	// Create pod in Kubernetes
	createdPod, err := m.k8sClient.CoreV1().Pods(m.namespace).Create(
		context.TODO(), pod, metav1.CreateOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	// Wait for pod to be ready and get its address
	address, err := m.waitForPodReady(createdPod.Name)
	if err != nil {
		// Clean up on failure
		_ = m.k8sClient.CoreV1().Pods(m.namespace).Delete(context.TODO(), createdPod.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("failed to wait for pod ready: %w", err)
	}

	// Create compute node record
	computeNode := &types.ComputeNode{
		ID:           computeID,
		ProjectID:    projectID,
		State:        types.StateActive,
		Address:      address,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		Config:       config,
	}

	if err := m.stateStore.CreateComputeNode(computeNode); err != nil {
		// Clean up pod on failure
		_ = m.k8sClient.CoreV1().Pods(m.namespace).Delete(context.TODO(), createdPod.Name, metav1.DeleteOptions{})
		return nil, fmt.Errorf("failed to save compute node: %w", err)
	}

	return computeNode, nil
}

// waitForPodReady waits for a pod to be ready and returns its address
func (m *Manager) waitForPodReady(podName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // Increased timeout for MariaDB
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Get pod status for debugging
			if pod, err := m.k8sClient.CoreV1().Pods(m.namespace).Get(context.Background(), podName, metav1.GetOptions{}); err == nil {
				return "", fmt.Errorf("timeout waiting for pod %s (phase: %s, reason: %s)", podName, pod.Status.Phase, pod.Status.Reason)
			}
			return "", ctx.Err()
		case <-ticker.C:
			pod, err := m.k8sClient.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check for pod scheduling issues
			if pod.Status.Phase == corev1.PodPending {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
						return "", fmt.Errorf("pod %s cannot be scheduled: %s", podName, condition.Reason)
					}
				}
				// Check for container image pull issues
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Waiting != nil {
						if status.State.Waiting.Reason == "ImagePullBackOff" || status.State.Waiting.Reason == "ErrImagePull" {
							return "", fmt.Errorf("pod %s image pull failed: %s", podName, status.State.Waiting.Message)
						}
					}
				}
			}

			// Check if pod is running and has an IP
			if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
				// Return IP - MariaDB may take time to be fully ready, but pod is running
				return fmt.Sprintf("%s:3306", pod.Status.PodIP), nil
			}

			// Pod is starting or running, continue waiting
			if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
				continue
			}

			// Check for eviction
			if pod.Status.Reason == "Evicted" {
				return "", fmt.Errorf("pod %s was evicted: %s", podName, pod.Status.Message)
			}

			// Pod failed or in error state
			if pod.Status.Phase == corev1.PodFailed {
				reason := pod.Status.Reason
				message := pod.Status.Message
				if message != "" {
					return "", fmt.Errorf("pod %s failed: %s - %s", podName, reason, message)
				}
				return "", fmt.Errorf("pod %s failed: %s", podName, reason)
			}
		}
	}
}

// SuspendComputeNode suspends a compute node
func (m *Manager) SuspendComputeNode(computeID string) error {
	// Update state to suspending
	if err := m.stateStore.UpdateComputeNodeState(computeID, types.StateSuspending); err != nil {
		return err
	}

	// Find pod by label
	pods, err := m.k8sClient.CoreV1().Pods(m.namespace).List(
		context.TODO(),
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("compute-id=%s", computeID),
		},
	)
	if err != nil {
		return err
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("pod not found for compute node %s", computeID)
	}

	pod := pods.Items[0]

	// Delete pod (Kubernetes will handle cleanup)
	if err := m.k8sClient.CoreV1().Pods(m.namespace).Delete(
		context.TODO(), pod.Name, metav1.DeleteOptions{},
	); err != nil {
		return err
	}

	// Update state to suspended
	return m.stateStore.UpdateComputeNodeState(computeID, types.StateSuspended)
}

// ResumeComputeNode resumes a suspended compute node
func (m *Manager) ResumeComputeNode(computeID string) (*types.ComputeNode, error) {
	// Update state to resuming
	if err := m.stateStore.UpdateComputeNodeState(computeID, types.StateResuming); err != nil {
		return nil, err
	}

	// Get compute node
	node, err := m.stateStore.GetComputeNode(computeID)
	if err != nil {
		return nil, err
	}

	// Check if pod still exists and wait for deletion if needed
	podName := fmt.Sprintf("compute-%s", computeID[:8])
	existingPod, err := m.k8sClient.CoreV1().Pods(m.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err == nil && existingPod != nil {
		// Pod exists, wait for it to be deleted
		if existingPod.DeletionTimestamp != nil {
			// Pod is being deleted, wait for it
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for {
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("timeout waiting for pod %s to be deleted", podName)
				default:
					_, err := m.k8sClient.CoreV1().Pods(m.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
					if err != nil {
						// Pod is deleted, break out of loop
						goto createPod
					}
					time.Sleep(500 * time.Millisecond)
				}
			}
		} else {
			// Pod exists but not being deleted, delete it first
			if err := m.k8sClient.CoreV1().Pods(m.namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{}); err != nil {
				return nil, fmt.Errorf("failed to delete existing pod: %w", err)
			}
			// Wait for deletion
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for {
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("timeout waiting for pod %s to be deleted", podName)
				default:
					_, err := m.k8sClient.CoreV1().Pods(m.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
					if err != nil {
						// Pod is deleted, break out of loop
						goto createPod
					}
					time.Sleep(500 * time.Millisecond)
				}
			}
		}
	}

createPod:
	// Recreate pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("compute-%s", computeID[:8]),
			Namespace: m.namespace,
			Labels: map[string]string{
				"app":        "mariadb-compute",
				"project-id": node.ProjectID,
				"compute-id": computeID,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "mariadb",
					Image: node.Config.Image,
					Env: []corev1.EnvVar{
						{Name: "PAGE_SERVER_URL", Value: node.Config.PageServerURL},
						{Name: "SAFEKEEPER_URL", Value: node.Config.SafekeeperURL},
						{Name: "PROJECT_ID", Value: node.ProjectID},
						{Name: "COMPUTE_ID", Value: computeID},
						{Name: "MYSQL_ROOT_PASSWORD", Value: "root"},
						{Name: "MYSQL_DATABASE", Value: "test"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(node.Config.Resources.CPU),
							corev1.ResourceMemory: resource.MustParse(node.Config.Resources.Memory),
						},
					},
					Ports: []corev1.ContainerPort{
						{ContainerPort: 3306, Name: "mysql"},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
			Tolerations: []corev1.Toleration{
				{
					Key:      "node.kubernetes.io/disk-pressure",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	// Create pod
	createdPod, err := m.k8sClient.CoreV1().Pods(m.namespace).Create(
		context.TODO(), pod, metav1.CreateOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to recreate pod: %w", err)
	}

	// Wait for pod to be ready
	address, err := m.waitForPodReady(createdPod.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for pod ready: %w", err)
	}

	// Update compute node
	node.Address = address
	node.LastActivity = time.Now()
	if err := m.stateStore.UpdateComputeNodeState(computeID, types.StateActive); err != nil {
		return nil, err
	}

	return node, nil
}

// DestroyComputeNode destroys a compute node
func (m *Manager) DestroyComputeNode(computeID string) error {
	// Find and delete pod
	pods, err := m.k8sClient.CoreV1().Pods(m.namespace).List(
		context.TODO(),
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("compute-id=%s", computeID),
		},
	)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if err := m.k8sClient.CoreV1().Pods(m.namespace).Delete(
			context.TODO(), pod.Name, metav1.DeleteOptions{},
		); err != nil {
			return err
		}
	}

	// Update state to terminated
	if err := m.stateStore.UpdateComputeNodeState(computeID, types.StateTerminated); err != nil {
		return err
	}

	// Delete from state store
	return m.stateStore.DeleteComputeNode(computeID)
}

// GetComputeNode retrieves a compute node
func (m *Manager) GetComputeNode(computeID string) (*types.ComputeNode, error) {
	return m.stateStore.GetComputeNode(computeID)
}

// GetComputeNodeByProject retrieves compute node for a project
func (m *Manager) GetComputeNodeByProject(projectID string) (*types.ComputeNode, error) {
	return m.stateStore.GetComputeNodeByProject(projectID)
}

// UpdateComputeNodeActivity updates the last activity time
func (m *Manager) UpdateComputeNodeActivity(computeID string) error {
	return m.stateStore.UpdateComputeNodeActivity(computeID)
}
