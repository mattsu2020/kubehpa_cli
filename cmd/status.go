package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "status NAME",
		Short:             "Show concise status for one HPA",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			includeInterpretation := (opts.interpret || opts.explain || opts.suggest) && !opts.noInterpret
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
		},
	}
}

func newAnalyzeCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:               "analyze NAME",
		Aliases:           []string{"diagnose"},
		Short:             "Analyze one HPA using visible Kubernetes API signals",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: hpaNameCompletion(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
}

func runStatus(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	report, err := buildStatusReport(ctx, opts, name, includeInterpretation)
	if err != nil {
		return err
	}
	if opts.apply {
		applied, err := applySuggestions(ctx, out, opts, name, report.Analysis.Suggestions)
		if err != nil {
			return err
		}
		report.Analysis.Actions = append(report.Analysis.Actions, applied...)
	}

	return writeOutput(out, opts.output, opts.template, report, func() error {
		return hpaanalysis.WriteStatusTextWithOptions(out, report, hpaanalysis.StatusTextOptions{
			Theme: style.NewTheme(shouldColorize(opts.color, out)),
			Lang:  outputLang(opts),
			Fix:   opts.fix,
		})
	})
}

func buildStatusReport(ctx context.Context, opts *options, name string, includeInterpretation bool) (hpaanalysis.StatusReport, error) {
	client, err := opts.newClient()
	if err != nil {
		return hpaanalysis.StatusReport{}, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	hpa, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(client.Namespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return hpaanalysis.StatusReport{}, fmt.Errorf("HPA %q was not found in namespace %q; check the name, namespace, or use list -A to find it: %w", name, client.Namespace, err)
		}
		return hpaanalysis.StatusReport{}, fmt.Errorf("failed to get HPA %s/%s from the Kubernetes API server: %w", client.Namespace, name, err)
	}

	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.AnalyzeWithOptions(hpa, includeInterpretation, analysisOptions(opts)),
	}

	if opts.events.enabled {
		events, err := hpaanalysis.RecentEvents(ctx, client.Interface, hpa.Namespace, hpa.Name, int64(opts.events.limit))
		if err != nil {
			report.Events = []hpaanalysis.Event{{Reason: "Error", Message: fmt.Sprintf("failed to list events: %v", err)}}
		} else {
			report.Events = events
		}
	}

	if opts.keda {
		report.Analysis.KEDAInfo = enrichKEDA(ctx, opts, hpa)
	}

	return report, nil
}

func enrichKEDA(ctx context.Context, opts *options, hpa *autoscalingv2.HorizontalPodAutoscaler) *hpaanalysis.KEDAAnalysis {
	isKEDA, _ := kube.DetectKEDA(hpa)
	if !isKEDA {
		return nil
	}

	dynClient, _, err := kube.NewDynamicClient(kube.Options{
		Namespace:  opts.namespace,
		Context:    opts.contextName,
		Kubeconfig: opts.kubeconfig,
		Cluster:    opts.cluster,
	})
	if err != nil {
		return &hpaanalysis.KEDAAnalysis{
			Lines: []string{fmt.Sprintf("[confidence: high] HPA appears KEDA-managed but dynamic client failed: %v", err)},
		}
	}

	scaledObject, err := kube.FindScaledObjectForHPA(ctx, dynClient, nil, hpa)
	if err != nil {
		return &hpaanalysis.KEDAAnalysis{
			Lines: []string{fmt.Sprintf("[confidence: high] HPA appears KEDA-managed but no ScaledObject found: %v", err)},
		}
	}

	info := kube.ExtractKEDAInfo(scaledObject)

	triggers := make([]hpaanalysis.KEDATriggerSummary, 0, len(info.Triggers))
	for _, t := range info.Triggers {
		triggers = append(triggers, hpaanalysis.KEDATriggerSummary{
			Type: t.Type,
			Name: t.Name,
		})
	}

	var conditionLines []string
	for _, c := range info.Conditions {
		if strings.EqualFold(c.Status, "False") {
			conditionLines = append(conditionLines, fmt.Sprintf("condition %q is False (reason: %s): %s", c.Type, c.Reason, c.Message))
		}
	}

	kedaAnalysis := &hpaanalysis.KEDAAnalysis{
		ScaledObjectName: info.ScaledObjectName,
		Triggers:         triggers,
		PollingInterval:  info.PollingInterval,
		CooldownPeriod:   info.CooldownPeriod,
		MinReplicaCount:  info.MinReplicaCount,
		MaxReplicaCount:  info.MaxReplicaCount,
		Lines:            conditionLines,
	}

	if len(conditionLines) == 0 && len(info.Conditions) > 0 {
		kedaAnalysis.Lines = []string{fmt.Sprintf("ScaledObject reports %d condition(s), all healthy.", len(info.Conditions))}
	}

	// Add KEDA interpretation lines to the analysis.
	kedaAnalysis.Lines = append(kedaAnalysis.Lines, hpaanalysis.AnalyzeKEDA(hpa, kedaAnalysis)...)

	return kedaAnalysis
}
