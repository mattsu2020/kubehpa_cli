package kube

import (
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDetectKEDA_ByLabel(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-hpa",
			Labels: map[string]string{"scaledobject.keda.sh/name": "worker"},
		},
	}
	isKEDA, name := DetectKEDA(hpa)
	if !isKEDA {
		t.Fatal("expected KEDA detection from label")
	}
	if name != "worker" {
		t.Fatalf("expected scaledObject name 'worker', got %q", name)
	}
}

func TestDetectKEDA_ByAnnotation(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-hpa",
			Annotations: map[string]string{"app.kubernetes.io/managed-by": "keda-operator"},
		},
	}
	isKEDA, _ := DetectKEDA(hpa)
	if !isKEDA {
		t.Fatal("expected KEDA detection from annotation")
	}
}

func TestDetectKEDA_ByNamePrefix(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: "keda-hpa-worker",
		},
	}
	isKEDA, name := DetectKEDA(hpa)
	if !isKEDA {
		t.Fatal("expected KEDA detection from name prefix")
	}
	if name != "worker" {
		t.Fatalf("expected scaledObject name 'worker', got %q", name)
	}
}

func TestDetectKEDA_NotKEDA(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-app-hpa",
		},
	}
	isKEDA, _ := DetectKEDA(hpa)
	if isKEDA {
		t.Fatal("expected no KEDA detection for plain HPA")
	}
}

func TestDetectKEDA_Nil(t *testing.T) {
	isKEDA, _ := DetectKEDA(nil)
	if isKEDA {
		t.Fatal("expected false for nil HPA")
	}
}

func TestExtractKEDAInfo(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "worker-so",
				"namespace": "default",
			},
			"spec": map[string]any{
				"pollingInterval": int64(30),
				"cooldownPeriod":  int64(300),
				"minReplicaCount": int64(1),
				"maxReplicaCount": int64(50),
				"triggers": []any{
					map[string]any{
						"type": "azure-queue",
						"metadata": map[string]any{
							"queueName": "orders",
						},
					},
				},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":    "Ready",
						"status":  "True",
						"reason":  "ScaledObjectReady",
						"message": "ScaledObject is ready",
					},
				},
			},
		},
	}

	info := ExtractKEDAInfo(u)

	if info.ScaledObjectName != "worker-so" {
		t.Fatalf("expected name 'worker-so', got %q", info.ScaledObjectName)
	}
	if info.PollingInterval == nil || *info.PollingInterval != 30 {
		t.Fatalf("expected pollingInterval 30, got %v", info.PollingInterval)
	}
	if info.CooldownPeriod == nil || *info.CooldownPeriod != 300 {
		t.Fatalf("expected cooldownPeriod 300, got %v", info.CooldownPeriod)
	}
	if info.MinReplicaCount == nil || *info.MinReplicaCount != 1 {
		t.Fatalf("expected minReplicaCount 1, got %v", info.MinReplicaCount)
	}
	if info.MaxReplicaCount == nil || *info.MaxReplicaCount != 50 {
		t.Fatalf("expected maxReplicaCount 50, got %v", info.MaxReplicaCount)
	}
	if len(info.Triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(info.Triggers))
	}
	if info.Triggers[0].Type != "azure-queue" {
		t.Fatalf("expected trigger type 'azure-queue', got %q", info.Triggers[0].Type)
	}
	if len(info.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(info.Conditions))
	}
}

func TestExtractKEDAInfo_Nil(t *testing.T) {
	info := ExtractKEDAInfo(nil)
	if info.ScaledObjectName != "" {
		t.Fatalf("expected empty name for nil input, got %q", info.ScaledObjectName)
	}
}
