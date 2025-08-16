package networkpolicy

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// convertServicePortsToPolicyPorts creates a slice of NetworkPolicyPort from a slice of ServicePort.
// This is a central helper to ensure consistency and correct port mapping.
func convertServicePortsToPolicyPorts(servicePorts []corev1.ServicePort) []networkingv1.NetworkPolicyPort {
	policyPorts := make([]networkingv1.NetworkPolicyPort, len(servicePorts))
	for i, port := range servicePorts {
		targetPort := port.TargetPort

		// If TargetPort is not specified in the Service spec, it defaults to the value of the Port field.
		// An empty IntOrString has Type=0, IntVal=0, and StrVal="".
		if targetPort.Type == intstr.Int && targetPort.IntVal == 0 && targetPort.StrVal == "" {
			targetPort = intstr.FromInt(int(port.Port))
		}

		protocol := port.Protocol
		policyPorts[i] = networkingv1.NetworkPolicyPort{
			Protocol: &protocol,
			Port:     &targetPort,
		}
	}
	return policyPorts
}

// DefineAccessNetworkPolicy creates a NetworkPolicy for pod-to-pod communication.
func DefineAccessNetworkPolicy(
	name, sourceNs string,
	sourcePodLabels, targetPodLabels map[string]string,
	targetNs string,
	ports []corev1.ServicePort,
	direction string,
) *networkingv1.NetworkPolicy {
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sourceNs,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: sourcePodLabels,
			},
		},
	}

	switch direction {
	case "ingress":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		netpol.Spec.Ingress = generateAccessIngressRule(targetPodLabels, targetNs, ports)
	case "egress":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}
		netpol.Spec.Egress = generateAccessEgressRule(targetPodLabels, targetNs, ports)
	case "all":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}
		netpol.Spec.Ingress = generateAccessIngressRule(targetPodLabels, targetNs, ports)
		netpol.Spec.Egress = generateAccessEgressRule(targetPodLabels, targetNs, ports)
	}

	return netpol
}

// generateAccessIngressRule creates an ingress rule allowing traffic from specific pods and namespaces.
func generateAccessIngressRule(
	targetPodLabels map[string]string,
	targetNs string,
	ports []corev1.ServicePort,
) []networkingv1.NetworkPolicyIngressRule {
	return []networkingv1.NetworkPolicyIngressRule{{
		Ports: convertServicePortsToPolicyPorts(ports),
		From: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNs},
			},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: targetPodLabels,
			},
		}},
	}}
}

// generateAccessEgressRule creates an egress rule allowing traffic to specific pods and namespaces.
func generateAccessEgressRule(
	targetPodLabels map[string]string,
	targetNs string,
	ports []corev1.ServicePort,
) []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{{
		Ports: convertServicePortsToPolicyPorts(ports),
		To: []networkingv1.NetworkPolicyPeer{{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNs},
			},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: targetPodLabels,
			},
		}},
	}}
}

// DefineExternalAccessNetworkPolicyCIDR creates a NetworkPolicy for traffic to/from an external CIDR.
func DefineExternalAccessNetworkPolicyCIDR(
	name, sourceNs string,
	sourcePodLabels map[string]string,
	cidr string,
	ports []corev1.ServicePort,
	direction string,
) *networkingv1.NetworkPolicy {
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: sourceNs,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: sourcePodLabels,
			},
		},
	}

	switch direction {
	case "ingress":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		netpol.Spec.Ingress = generateExternalAccessIngressRuleCIDR(cidr, ports)
	case "egress":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}
		netpol.Spec.Egress = generateExternalAccessEgressRuleCIDR(cidr, ports)
	case "all":
		netpol.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}
		netpol.Spec.Ingress = generateExternalAccessIngressRuleCIDR(cidr, ports)
		netpol.Spec.Egress = generateExternalAccessEgressRuleCIDR(cidr, ports)
	}

	return netpol
}

// generateExternalAccessIngressRuleCIDR creates ingress rules for traffic from a CIDR block.
func generateExternalAccessIngressRuleCIDR(
	cidr string,
	ports []corev1.ServicePort,
) []networkingv1.NetworkPolicyIngressRule {
	return []networkingv1.NetworkPolicyIngressRule{{
		Ports: convertServicePortsToPolicyPorts(ports),
		From: []networkingv1.NetworkPolicyPeer{{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		}},
	}}
}

// generateExternalAccessEgressRuleCIDR creates egress rules for traffic to a CIDR block.
func generateExternalAccessEgressRuleCIDR(
	cidr string,
	ports []corev1.ServicePort,
) []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{{
		Ports: convertServicePortsToPolicyPorts(ports),
		To: []networkingv1.NetworkPolicyPeer{{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		}},
	}}
}

// ExtractNetpolSpec is a utility function to extract the spec from a NetworkPolicy object.
func ExtractNetpolSpec(obj client.Object) any {
	return obj.(*networkingv1.NetworkPolicy).Spec
}
