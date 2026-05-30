package hpa

import (
	"fmt"
	"io"
)

type StatusReport struct {
	Analysis Analysis `json:"analysis" yaml:"analysis"`
	Events   []Event  `json:"events,omitempty" yaml:"events,omitempty"`
}

func WriteStatusText(w io.Writer, report StatusReport) {
	a := report.Analysis
	fmt.Fprintf(w, "HPA %s/%s\n", a.Namespace, a.Name)
	fmt.Fprintf(w, "Target: %s\n", a.Target)
	fmt.Fprintf(w, "Replicas: current=%d desired=%d min=%d max=%d\n", a.Current, a.Desired, a.Min, a.Max)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Summary: %s\n", a.Summary)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Conditions:")
	if len(a.Conditions) == 0 {
		fmt.Fprintln(w, "  No conditions reported.")
	} else {
		for _, condition := range a.Conditions {
			fmt.Fprintf(w, "  %-15s %-7s %-24s %s\n", condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Metrics:")
	if len(a.Metrics) == 0 {
		fmt.Fprintln(w, "  No current metrics reported.")
	} else {
		for _, metric := range a.Metrics {
			fmt.Fprintf(w, "  - %s\n", metric.Text)
		}
	}

	if len(a.Interpretation) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Interpretation:")
		for _, line := range a.Interpretation {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Recent events:")
	if len(report.Events) == 0 {
		fmt.Fprintln(w, "  No recent events found.")
	} else {
		for _, event := range report.Events {
			fmt.Fprintf(w, "  - %s: %s\n", event.Reason, event.Message)
		}
	}
}

type ListItem struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Name      string `json:"name" yaml:"name"`
	Target    string `json:"target" yaml:"target"`
	Current   int32  `json:"currentReplicas" yaml:"currentReplicas"`
	Desired   int32  `json:"desiredReplicas" yaml:"desiredReplicas"`
	Min       int32  `json:"minReplicas" yaml:"minReplicas"`
	Max       int32  `json:"maxReplicas" yaml:"maxReplicas"`
	Summary   string `json:"summary" yaml:"summary"`
	Issue     string `json:"issue,omitempty" yaml:"issue,omitempty"`
}

type ListReport struct {
	Items []ListItem `json:"items" yaml:"items"`
}

func NewListItem(src Analysis) ListItem {
	issue := ""
	for _, condition := range src.Conditions {
		if condition.Type == "ScalingActive" && condition.Status != "True" {
			issue = condition.Reason
			break
		}
		if condition.Type == "ScalingLimited" && condition.Status == "True" {
			issue = condition.Reason
		}
	}
	return ListItem{
		Namespace: src.Namespace,
		Name:      src.Name,
		Target:    src.Target,
		Current:   src.Current,
		Desired:   src.Desired,
		Min:       src.Min,
		Max:       src.Max,
		Summary:   src.Summary,
		Issue:     issue,
	}
}

func WriteListText(w io.Writer, report ListReport, wide bool) {
	if wide {
		fmt.Fprintf(w, "%-20s %-32s %-28s %-8s %-8s %-8s %-8s %-24s %s\n", "NAMESPACE", "NAME", "TARGET", "CURRENT", "DESIRED", "MIN", "MAX", "ISSUE", "SUMMARY")
		for _, item := range report.Items {
			fmt.Fprintf(w, "%-20s %-32s %-28s %-8d %-8d %-8d %-8d %-24s %s\n", item.Namespace, item.Name, item.Target, item.Current, item.Desired, item.Min, item.Max, item.Issue, item.Summary)
		}
		return
	}

	fmt.Fprintf(w, "%-20s %-32s %-8s %-8s %-24s %s\n", "NAMESPACE", "NAME", "CURRENT", "DESIRED", "ISSUE", "SUMMARY")
	for _, item := range report.Items {
		fmt.Fprintf(w, "%-20s %-32s %-8d %-8d %-24s %s\n", item.Namespace, item.Name, item.Current, item.Desired, item.Issue, item.Summary)
	}
}
