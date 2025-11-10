package multitenancy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// NetworkPolicyManager manages network policies for multi-tenancy isolation
// Mimics Neon's approach to project isolation
type NetworkPolicyManager struct {
	k8sClient kubernetes.Interface
	namespace string
}

// NewNetworkPolicyManager creates a new network policy manager
func NewNetworkPolicyManager(k8sClient kubernetes.Interface, namespace string) *NetworkPolicyManager {
	return &NetworkPolicyManager{
		k8sClient: k8sClient,
		namespace: namespace,
	}
}

// CreateProjectNetworkPolicy creates a network policy to isolate a project
// This ensures compute nodes can only communicate with their project's Page Server and Safekeeper
func (npm *NetworkPolicyManager) CreateProjectNetworkPolicy(projectID string, pageServerURL, safekeeperURL string) error {
	// Note: In production, we'd extract hosts from URLs for more specific policies
	// For now, we use pod selectors

	// Create network policy
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("project-%s-isolation", projectID),
			Namespace: npm.namespace,
			Labels: map[string]string{
				"project-id": projectID,
				"app":        "mariadb-compute",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"project-id": projectID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				// Allow ingress from control plane
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"name": "control-plane",
								},
							},
						},
					},
				},
				// Allow ingress from proxy
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "proxy",
								},
							},
						},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// Allow egress to Page Server
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "page-server",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 8081},
						},
					},
				},
				// Allow egress to Safekeeper
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "safekeeper",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 8082},
						},
					},
				},
				// Allow DNS
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"name": "kube-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: func() *corev1.Protocol { p := corev1.ProtocolUDP; return &p }(),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 53},
						},
					},
				},
			},
		},
	}

	// Create or update network policy
	_, err := npm.k8sClient.NetworkingV1().NetworkPolicies(npm.namespace).Create(
		context.TODO(), policy, metav1.CreateOptions{},
	)
	if err != nil {
		// Try to update if it already exists
		_, err = npm.k8sClient.NetworkingV1().NetworkPolicies(npm.namespace).Update(
			context.TODO(), policy, metav1.UpdateOptions{},
		)
		return err
	}

	return nil
}

// DeleteProjectNetworkPolicy deletes the network policy for a project
func (npm *NetworkPolicyManager) DeleteProjectNetworkPolicy(projectID string) error {
	policyName := fmt.Sprintf("project-%s-isolation", projectID)
	return npm.k8sClient.NetworkingV1().NetworkPolicies(npm.namespace).Delete(
		context.TODO(), policyName, metav1.DeleteOptions{},
	)
}


