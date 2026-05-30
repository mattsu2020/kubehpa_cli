package hpa

import (
	"fmt"
	"sort"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const limitation = "This plugin uses existing HPA status, conditions, metrics, and events. It does not expose internal controller calculations."

type Analysis struct {
	Namespace      string             `json:"namespace" yaml:"namespace"`
	Name           string             `json:"name" yaml:"name"`
	Target         string             `json:"target" yaml:"target"`
	Current        int32              `json:"currentReplicas" yaml:"currentReplicas"`
	Desired        int32              `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min            int32              `json:"minReplicas" yaml:"minReplicas"`
	Max            int32              `json:"maxReplicas" yaml:"maxReplicas"`
	Summary        string             `json:"summary" yaml:"summary"`
	Conditions     []Condition        `json:"conditions" yaml:"conditions"`
	Metrics        []Metric           `json:"metrics" yaml:"metrics"`
	Interpretation []string           `json:"interpretation,omitempty" yaml:"interpretation,omitempty"`
	ImpactMetric   *MetricImpactGuess `json:"impactMetric,omitempty" yaml:"impactMetric,omitempty"`
}

type Condition struct {
	Type    string `json:"type" yaml:"type"`
	Status  string `json:"status" yaml:"status"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

type Metric struct {
	Type    string   `json:"type" yaml:"type"`
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Current string   `json:"current,omitempty" yaml:"current,omitempty"`
	Target  string   `json:"target,omitempty" yaml:"target,omitempty"`
	Ratio   *float64 `json:"ratio,omitempty" yaml:"ratio,omitempty"`
	Note    string   `json:"note,omitempty" yaml:"note,omitempty"`
	Text    string   `json:"text" yaml:"text"`
}

type MetricImpactGuess struct {
	Name  string  `json:"name" yaml:"name"`
	Ratio float64 `json:"ratio" yaml:"ratio"`
	Note  string  `json:"note" yaml:"note"`
}

func Analyze(src *autoscalingv2.HorizontalPodAutoscaler, includeInterpretation bool) Analysis {
	minReplicas := int32(1)
	if src.Spec.MinReplicas != nil {
		minReplicas = *src.Spec.MinReplicas
	}

	analysis := Analysis{
		Namespace: src.Namespace,
		Name:      src.Name,
		Target:    fmt.Sprintf("%s/%s", src.Spec.ScaleTargetRef.Kind, src.Spec.ScaleTargetRef.Name),
		Current:   src.Status.CurrentReplicas,
		Desired:   src.Status.DesiredReplicas,
		Min:       minReplicas,
		Max:       src.Spec.MaxReplicas,
		Summary:   SummarizeDirection(src, minReplicas),
	}

	for _, condition := range prioritizedConditions(src.Status.Conditions) {
		analysis.Conditions = append(analysis.Conditions, Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}

	for _, metric := range src.Status.CurrentMetrics {
		analysis.Metrics = append(analysis.Metrics, FormatMetricStatus(src, metric))
	}

	if guess, ok := MostInfluentialMetric(src); ok {
		analysis.ImpactMetric = &guess
	}

	if includeInterpretation {
		analysis.Interpretation = Interpret(src, minReplicas)
	}

	return analysis
}

func SummarizeDirection(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) string {
	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		return "HPA cannot currently compute a scaling recommendation from metrics."
	}
	if hpa.Status.DesiredReplicas == 0 && hpa.Status.CurrentReplicas > 0 {
		return "HPA has no visible desired replica recommendation in status."
	}

	current := hpa.Status.CurrentReplicas
	desired := hpa.Status.DesiredReplicas

	switch {
	case desired > current:
		return "HPA currently wants to scale up."
	case desired < current:
		return "HPA currently wants to scale down."
	case desired == hpa.Spec.MaxReplicas:
		return "HPA is at maxReplicas."
	case desired == minReplicas:
		return "HPA is at minReplicas."
	default:
		return "HPA currently keeps the replica count unchanged."
	}
}

func Interpret(hpa *autoscalingv2.HorizontalPodAutoscaler, minReplicas int32) []string {
	var lines []string

	if hpa.Status.ObservedGeneration != nil && *hpa.Status.ObservedGeneration < hpa.Generation {
		lines = append(lines, fmt.Sprintf("Warning: status.observedGeneration=%d is behind metadata.generation=%d; the status may not reflect the latest spec.", *hpa.Status.ObservedGeneration, hpa.Generation))
	}

	if condition := FindCondition(hpa, "ScalingActive"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("ScalingActive is %s: %s - %s", condition.Status, condition.Reason, condition.Message),
			"The HPA is not reporting a reliable scale direction while metric evaluation is inactive.",
			"This plugin avoids treating desiredReplicas=0 as a scale-down recommendation in this state.",
			limitation,
		)
		return lines
	}

	if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Status != corev1.ConditionTrue {
		lines = append(lines,
			fmt.Sprintf("AbleToScale is %s: %s - %s", condition.Status, condition.Reason, condition.Message))
	} else if condition := FindCondition(hpa, "AbleToScale"); condition != nil && condition.Reason == "ScaleDownStabilized" {
		lines = append(lines,
			fmt.Sprintf("Scale down appears stabilized: %s", condition.Message))
	}

	if condition := FindCondition(hpa, "ScalingLimited"); condition != nil && condition.Status == corev1.ConditionTrue {
		switch hpa.Status.DesiredReplicas {
		case hpa.Spec.MaxReplicas:
			lines = append(lines, "ScalingLimited reports that the visible desired replica count is constrained by maxReplicas.")
		case minReplicas:
			lines = append(lines, "ScalingLimited reports that the visible desired replica count is constrained by minReplicas.")
		default:
			lines = append(lines, "The recommendation is reported as limited.")
		}
	}

	if hpa.Status.DesiredReplicas > hpa.Status.CurrentReplicas {
		lines = append(lines, "desiredReplicas is greater than currentReplicas, so the HPA is recommending scale up.")
	} else if hpa.Status.DesiredReplicas < hpa.Status.CurrentReplicas {
		lines = append(lines, "desiredReplicas is less than currentReplicas, so the HPA is recommending scale down.")
	} else {
		lines = append(lines, "desiredReplicas equals currentReplicas, so no immediate replica change is visible from status.")
		if hpa.Status.DesiredReplicas != hpa.Spec.MaxReplicas && hpa.Status.DesiredReplicas != minReplicas {
			if metric, ok := MetricOutsideTarget(hpa); ok {
				lines = append(lines, fmt.Sprintf("%s metric ratio is approximately %.3f, which is close to the target.", metric.Name, metric.Ratio))
				lines = append(lines, "This is consistent with tolerance-based no-scale. Kubernetes commonly uses a tolerance band around the target, but HPA status does not expose tolerance as an explicit reason.")
				lines = append(lines, "The plugin avoids claiming the exact internal reason because rounding, stabilization, or conservative metric handling may also affect the final result.")
			}
		}
	}

	if guess, ok := MostInfluentialMetric(hpa); ok && len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, fmt.Sprintf("Among visible resource utilization metrics, %s has the largest distance from target (ratio %.3f).", guess.Name, guess.Ratio))
		lines = append(lines, "This is only an impact estimate; the API does not expose per-metric replica recommendations or the final metric winner.")
	} else if len(hpa.Status.CurrentMetrics) > 1 {
		lines = append(lines, "Multiple current metrics are reported, but the API does not expose per-metric replica recommendations or which metric would have selected the recommendation before replica limits were applied.")
		lines = append(lines, "Events and human-readable messages can hint at the contributing metric, but they are not a stable decision record.")
	}

	lines = append(lines, limitation)

	return lines
}

func FindCondition(hpa *autoscalingv2.HorizontalPodAutoscaler, conditionType string) *autoscalingv2.HorizontalPodAutoscalerCondition {
	for i := range hpa.Status.Conditions {
		if string(hpa.Status.Conditions[i].Type) == conditionType {
			return &hpa.Status.Conditions[i]
		}
	}
	return nil
}

func FormatMetricStatus(hpa *autoscalingv2.HorizontalPodAutoscaler, metric autoscalingv2.MetricStatus) Metric {
	switch metric.Type {
	case "":
		return Metric{Text: "Metric status is present, but details are unavailable"}
	case autoscalingv2.ResourceMetricSourceType:
		if metric.Resource == nil {
			return Metric{Type: "Resource", Text: "Resource metric: <missing status>"}
		}
		target := FindResourceTarget(hpa, string(metric.Resource.Name))
		current := FormatMetricValue(metric.Resource.Current.AverageUtilization, metric.Resource.Current.AverageValue)
		note := CompareMetricToTarget(metric.Resource.Current.AverageUtilization, target)
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, target)
		text := fmt.Sprintf("Resource %s current=%s target=%s", metric.Resource.Name, current, target)
		if note != "" {
			text = fmt.Sprintf("%s note=%q", text, note)
		}
		return Metric{
			Type:    "Resource",
			Name:    string(metric.Resource.Name),
			Current: current,
			Target:  target,
			Ratio:   ratio,
			Note:    note,
			Text:    text,
		}
	default:
		return Metric{
			Type: string(metric.Type),
			Text: fmt.Sprintf("%s metric is present, but this POC only formats Resource metrics in detail", metric.Type),
		}
	}
}

func FindResourceTarget(hpa *autoscalingv2.HorizontalPodAutoscaler, name string) string {
	for _, spec := range hpa.Spec.Metrics {
		if spec.Type == autoscalingv2.ResourceMetricSourceType &&
			spec.Resource != nil &&
			string(spec.Resource.Name) == name {
			target := spec.Resource.Target
			switch target.Type {
			case autoscalingv2.UtilizationMetricType:
				if target.AverageUtilization != nil {
					return fmt.Sprintf("%d%%", *target.AverageUtilization)
				}
			case autoscalingv2.AverageValueMetricType:
				if target.AverageValue != nil {
					return target.AverageValue.String()
				}
			case autoscalingv2.ValueMetricType:
				if target.Value != nil {
					return target.Value.String()
				}
			}
		}
	}
	return "<unknown>"
}

func FormatMetricValue(utilization *int32, averageValue *resource.Quantity) string {
	if utilization != nil {
		return fmt.Sprintf("%d%%", *utilization)
	}
	if averageValue != nil && !averageValue.IsZero() {
		return averageValue.String()
	}
	return "<unknown>"
}

func CompareMetricToTarget(utilization *int32, target string) string {
	if utilization == nil || !strings.HasSuffix(target, "%") {
		return ""
	}

	targetUtilization, ok := parsePercent(target)
	if !ok {
		return ""
	}

	switch {
	case *utilization > targetUtilization:
		return "current value is above target"
	case *utilization < targetUtilization:
		return "current value is below target"
	default:
		return "current value equals target"
	}
}

func MetricOutsideTarget(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio != nil && *ratio != 1 {
			return MetricImpactGuess{Name: string(metric.Resource.Name), Ratio: *ratio}, true
		}
	}

	return MetricImpactGuess{}, false
}

func MostInfluentialMetric(hpa *autoscalingv2.HorizontalPodAutoscaler) (MetricImpactGuess, bool) {
	var best MetricImpactGuess
	var bestDistance float64

	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
			continue
		}
		ratio := utilizationRatio(metric.Resource.Current.AverageUtilization, FindResourceTarget(hpa, string(metric.Resource.Name)))
		if ratio == nil {
			continue
		}
		distance := *ratio - 1
		if distance < 0 {
			distance = -distance
		}
		if distance > bestDistance {
			bestDistance = distance
			best = MetricImpactGuess{
				Name:  string(metric.Resource.Name),
				Ratio: *ratio,
				Note:  "largest visible utilization ratio distance from target",
			}
		}
	}

	return best, bestDistance > 0
}

func prioritizedConditions(conditions []autoscalingv2.HorizontalPodAutoscalerCondition) []autoscalingv2.HorizontalPodAutoscalerCondition {
	out := append([]autoscalingv2.HorizontalPodAutoscalerCondition(nil), conditions...)
	priority := map[autoscalingv2.HorizontalPodAutoscalerConditionType]int{
		"ScalingActive":  0,
		"AbleToScale":    1,
		"ScalingLimited": 2,
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := priority[out[i].Type]
		right := priority[out[j].Type]
		if _, ok := priority[out[i].Type]; !ok {
			left = 100
		}
		if _, ok := priority[out[j].Type]; !ok {
			right = 100
		}
		return left < right
	})
	return out
}

func utilizationRatio(utilization *int32, target string) *float64 {
	if utilization == nil {
		return nil
	}
	targetUtilization, ok := parsePercent(target)
	if !ok || targetUtilization == 0 {
		return nil
	}
	ratio := float64(*utilization) / float64(targetUtilization)
	return &ratio
}

func parsePercent(value string) (int32, bool) {
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	var percent int32
	if _, err := fmt.Sscanf(strings.TrimSuffix(value, "%"), "%d", &percent); err != nil {
		return 0, false
	}
	return percent, true
}
