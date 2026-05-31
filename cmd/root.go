package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/mattsu2020/kubectl-hpa-status/internal/kube"
	"github.com/mattsu2020/kubectl-hpa-status/internal/style"
	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/yaml"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type options struct {
	namespace      string
	allNamespaces  bool
	contextName    string
	kubeconfig     string
	cluster        string
	output         string
	template       string
	wide           bool
	sortBy         string
	filter         string
	healthScoreMax int
	color          string
	events         eventOption
	interpret      bool
	noInterpret    bool
	explain        bool
	suggest        bool
	fix            bool
	apply          bool
	dryRun         bool
	yes            bool
	lang           string
	debug          bool
	config         string
	healthWeights  hpaanalysis.HealthWeights
	problem        bool
	recommend      bool
	watch          bool
	watchInterval  time.Duration
	watchTimeout   time.Duration
	untilCondition string
	clientOverride kubernetes.Interface
	in             io.Reader
}

func (o *options) newClient() (*kube.Client, error) {
	kopts := kube.Options{
		Namespace:  o.namespace,
		Context:    o.contextName,
		Kubeconfig: o.kubeconfig,
		Cluster:    o.cluster,
	}
	if o.clientOverride != nil {
		return kube.NewClient(kopts, kube.WithInterface(o.clientOverride))
	}
	return kube.NewClient(kopts)
}

func NewRootCommand() *cobra.Command {
	opts := &options{
		events:         eventOption{enabled: true, limit: 5},
		color:          "auto",
		dryRun:         true,
		healthScoreMax: -1,
		watchInterval:  5 * time.Second,
	}

	root := &cobra.Command{
		Use:           "kubectl-hpa-status",
		Short:         "Inspect HorizontalPodAutoscaler status",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if opts.recommend {
				opts.suggest = true
			}
			if opts.fix || opts.apply {
				opts.suggest = true
				opts.explain = true
			}
			if opts.noInterpret {
				opts.interpret = false
				opts.suggest = false
			}
			if opts.config != "" {
				weights, err := loadHealthWeights(opts.config)
				if err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to load --config %s: %v\n", opts.config, err)
				} else {
					opts.healthWeights = weights
				}
			}
			opts.in = cmd.InOrStdin()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			includeInterpretation := (opts.interpret || opts.explain || opts.suggest) && !opts.noInterpret
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], includeInterpretation)
		},
	}

	root.PersistentFlags().StringVarP(&opts.namespace, "namespace", "n", "", "namespace")
	root.PersistentFlags().BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "list HPAs across all namespaces")
	root.PersistentFlags().StringVar(&opts.contextName, "context", "", "kubeconfig context")
	root.PersistentFlags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	root.PersistentFlags().StringVar(&opts.cluster, "cluster", "", "kubeconfig cluster")
	root.PersistentFlags().StringVarP(&opts.output, "output", "o", "", "output format: table, wide, json, yaml, jsonpath=..., template=...")
	root.PersistentFlags().StringVar(&opts.template, "template", "", "template string to use when -o jsonpath or -o go-template/template is specified")
	root.PersistentFlags().BoolVar(&opts.wide, "wide", false, "show additional columns in table output")
	root.PersistentFlags().StringVar(&opts.color, "color", opts.color, "colorize table output: auto, always, never")
	root.PersistentFlags().BoolVar(&opts.interpret, "interpret", false, "include interpretation in status output")
	root.PersistentFlags().BoolVar(&opts.explain, "explain", false, "include detailed interpretation and recommended actions")
	root.PersistentFlags().BoolVar(&opts.suggest, "suggest", false, "include concrete suggestions for configuration changes")
	root.PersistentFlags().BoolVar(&opts.fix, "fix", false, "show stronger fix plan with patch commands")
	root.PersistentFlags().BoolVar(&opts.apply, "apply", false, "run suggested HPA spec patch workflow")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", opts.dryRun, "use server-side dry-run for --apply; set --dry-run=false to persist changes")
	root.PersistentFlags().BoolVarP(&opts.yes, "yes", "y", false, "skip confirmation when used with --apply")
	root.PersistentFlags().StringVar(&opts.lang, "lang", "", "text output language: en or ja")
	root.PersistentFlags().BoolVarP(&opts.debug, "debug", "v", false, "include internal analysis details such as ratios and health scoring inputs")
	root.PersistentFlags().StringVar(&opts.config, "config", "", "optional config file for analysis settings such as health score weights")
	root.PersistentFlags().BoolVar(&opts.recommend, "recommend", false, "alias for --suggest")
	root.PersistentFlags().BoolVar(&opts.noInterpret, "no-interpret", false, "omit interpretation and show raw status-derived data")
	root.PersistentFlags().Var(&opts.events, "events", "show recent HPA events: true, false, or a number")
	root.PersistentFlags().BoolVarP(&opts.watch, "watch", "w", false, "watch HPA status periodically")
	root.PersistentFlags().DurationVar(&opts.watchInterval, "interval", opts.watchInterval, "watch refresh interval")
	root.PersistentFlags().DurationVar(&opts.watchTimeout, "timeout", 0, "stop watching after this duration")
	root.PersistentFlags().StringVar(&opts.untilCondition, "until-condition", "", "stop watching once an HPA condition type is present, for example scaling-limited")
	root.PersistentFlags().Lookup("events").NoOptDefVal = "true"

	root.AddCommand(newStatusCommand(opts))
	root.AddCommand(newAnalyzeCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newWatchCommand(opts))
	root.AddCommand(newVersionCommand())
	root.AddCommand(newCompletionCommand(root))

	return root
}

func buildVersion() string {
	return fmt.Sprintf("%s (commit=%s, date=%s)", version, commit, date)
}

func newStatusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show concise status for one HPA",
		Args:  cobra.ExactArgs(1),
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
		Use:     "analyze NAME",
		Aliases: []string{"diagnose"},
		Short:   "Analyze one HPA using visible Kubernetes API signals",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
}

func newListCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List HPAs and highlight visible issues",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.watch {
				return runWatchList(cmd.Context(), cmd.OutOrStdout(), opts)
			}
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.sortBy, "sort-by", "", "sort list by namespace, name, current, desired, diff, health-score, or issue")
	cmd.Flags().StringVar(&opts.filter, "filter", "", "filter list by all, ok, error, limited, scaling-limited, or issue")
	cmd.Flags().IntVar(&opts.healthScoreMax, "health-score", -1, "show only HPAs with health score at or below this threshold")
	cmd.Flags().BoolVar(&opts.problem, "problem", false, "show only HPAs with visible problems")
	return cmd
}

func newScanCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "scan",
		Aliases: []string{"problems"},
		Short:   "Scan all namespaces for HPAs with visible problems",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.allNamespaces = true
			opts.problem = true
			opts.wide = true
			return runList(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
}

func newWatchCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch NAME",
		Short: "Watch one HPA status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cmd.OutOrStdout(), opts, args[0], !opts.noInterpret)
		},
	}
	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "kubectl-hpa-status version %s\n", buildVersion())
			return err
		},
	}
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	return cmd
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

	return report, nil
}

func runList(ctx context.Context, out io.Writer, opts *options) error {
	client, err := opts.newClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}

	namespace := client.Namespace
	if opts.allNamespaces {
		namespace = metav1.NamespaceAll
	}
	filter := opts.filter
	if opts.problem && filter == "" {
		filter = "issue"
	}

	hpas, err := client.Interface.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list HPAs: %w", err)
	}

	report := hpaanalysis.ListReport{}
	for i := range hpas.Items {
		item := hpaanalysis.NewListItem(hpaanalysis.AnalyzeWithOptions(&hpas.Items[i], false, analysisOptions(opts)))
		if matchesListFilter(item, filter) && matchesHealthScoreThreshold(item, opts.healthScoreMax) {
			report.Items = append(report.Items, item)
		}
	}
	sortBy := opts.sortBy
	if opts.problem && sortBy == "" {
		sortBy = "problem"
	}
	sortListItems(report.Items, sortBy)

	wide := opts.wide || opts.output == "wide"
	return writeOutput(out, opts.output, opts.template, report, func() error {
		return hpaanalysis.WriteListText(out, report, hpaanalysis.ListTextOptions{
			Wide:  wide,
			Color: shouldColorize(opts.color, out),
			Theme: style.NewTheme(shouldColorize(opts.color, out)),
			Lang:  outputLang(opts),
		})
	})
}

func runWatch(ctx context.Context, out io.Writer, opts *options, name string, includeInterpretation bool) error {
	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(opts.watchInterval)
	defer ticker.Stop()

	var previous *hpaanalysis.Analysis
	for {
		// Clear screen when writing to a terminal (theme is enabled)
		if clear := theme.ScreenClear(); clear != "" {
			if _, err := out.Write([]byte(clear)); err != nil {
				return err
			}
		} else {
			// Append-only for piped output
			if _, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return err
			}
		}

		report, err := buildStatusReport(ctx, opts, name, includeInterpretation)
		if err != nil {
			return err
		}
		if err := writeOutput(out, opts.output, opts.template, report, func() error {
			if previous != nil {
				return hpaanalysis.WriteStatusDiff(out, hpaanalysis.WatchState{
					Previous: previous,
					Current:  &report.Analysis,
				}, theme)
			}
			return hpaanalysis.WriteStatusText(out, report, theme)
		}); err != nil {
			return err
		}
		previous = &report.Analysis

		if opts.untilCondition != "" && reportHasCondition(report, opts.untilCondition) {
			_, err := fmt.Fprintf(out, "\nStopped: condition %q is present.\n", opts.untilCondition)
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
}

func runWatchList(ctx context.Context, out io.Writer, opts *options) error {
	if opts.watchTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.watchTimeout)
		defer cancel()
	}

	theme := style.NewTheme(shouldColorize(opts.color, out))
	ticker := time.NewTicker(opts.watchInterval)
	defer ticker.Stop()

	for {
		// Clear screen when writing to a terminal (theme is enabled)
		if clear := theme.ScreenClear(); clear != "" {
			if _, err := out.Write([]byte(clear)); err != nil {
				return err
			}
		} else {
			// Append-only for piped output
			if _, err := fmt.Fprintf(out, "Updated: %s\n\n", time.Now().Format(time.RFC3339)); err != nil {
				return err
			}
		}

		if err := runList(ctx, out, opts); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
	}
}

func matchesListFilter(item hpaanalysis.ListItem, filter string) bool {
	switch normalizeSelector(filter) {
	case "", "all":
		return true
	case "ok":
		return item.Health == "OK"
	case "error":
		return item.Health == "ERROR"
	case "limited", "scalinglimited":
		return item.Health == "LIMITED"
	case "issue":
		return item.Issue != ""
	default:
		return strings.EqualFold(item.Health, filter) || strings.Contains(normalizeSelector(item.Issue), normalizeSelector(filter))
	}
}

func matchesHealthScoreThreshold(item hpaanalysis.ListItem, threshold int) bool {
	if threshold <= 0 {
		return true
	}
	if threshold > 100 {
		threshold = 100
	}
	return item.HealthScore <= threshold
}

func sortListItems(items []hpaanalysis.ListItem, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		switch normalizeSelector(sortBy) {
		case "namespace":
			return left.Namespace < right.Namespace
		case "name", "":
			return left.Name < right.Name
		case "current", "currentreplicas":
			return left.Current < right.Current
		case "desired", "desiredreplicas":
			return left.Desired < right.Desired
		case "diff", "replicadiff", "difference":
			diffLeft := left.Desired - left.Current
			if diffLeft < 0 {
				diffLeft = -diffLeft
			}
			diffRight := right.Desired - right.Current
			if diffRight < 0 {
				diffRight = -diffRight
			}
			return diffLeft > diffRight // Descending order (largest diff first)
		case "age", "creationtimestamp":
			return left.CreationTimestamp.Before(&right.CreationTimestamp)
		case "health":
			return left.Health < right.Health
		case "healthscore", "score":
			return left.HealthScore > right.HealthScore
		case "problem":
			if left.HealthScore != right.HealthScore {
				return left.HealthScore < right.HealthScore
			}
			diffLeft := left.Desired - left.Current
			if diffLeft < 0 {
				diffLeft = -diffLeft
			}
			diffRight := right.Desired - right.Current
			if diffRight < 0 {
				diffRight = -diffRight
			}
			if diffLeft != diffRight {
				return diffLeft > diffRight
			}
			return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
		case "issue":
			return left.Issue < right.Issue
		case "min", "minreplicas":
			return left.Min < right.Min
		case "max", "maxreplicas":
			return left.Max < right.Max
		case "target":
			return left.Target < right.Target
		default:
			return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
		}
	})
}

func outputLang(opts *options) string {
	if opts.lang != "" {
		return strings.ToLower(opts.lang)
	}
	if strings.EqualFold(opts.output, "ja") {
		return "ja"
	}
	return ""
}

func analysisOptions(opts *options) hpaanalysis.AnalysisOptions {
	return hpaanalysis.AnalysisOptions{
		HealthWeights: opts.healthWeights,
		Debug:         opts.debug,
	}
}

func loadHealthWeights(path string) (hpaanalysis.HealthWeights, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hpaanalysis.HealthWeights{}, err
	}
	var cfg struct {
		HealthWeights hpaanalysis.HealthWeights `json:"healthWeights" yaml:"healthWeights"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return hpaanalysis.HealthWeights{}, err
	}
	return cfg.HealthWeights, nil
}

func applySuggestions(ctx context.Context, out io.Writer, opts *options, name string, suggestions []hpaanalysis.Suggestion) ([]string, error) {
	var patches []hpaanalysis.Suggestion
	for _, suggestion := range suggestions {
		if suggestion.Apply && suggestion.Patch != "" {
			patches = append(patches, suggestion)
		}
	}
	if len(patches) == 0 {
		return []string{"No applicable HPA patch was suggested."}, nil
	}
	client, err := opts.newClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client from kubeconfig/context flags: %w", err)
	}
	for _, suggestion := range patches {
		current, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get HPA before applying suggested patch %q: %w", suggestion.Title, err)
		}
		if _, err := fmt.Fprintf(out, "\nProposed patch: %s\n%s\n", suggestion.Title, patchDiff(current.Spec.MinReplicas, current.Status.DesiredReplicas, current.Spec.MaxReplicas, suggestion.Patch)); err != nil {
			return nil, err
		}
	}
	if opts.dryRun {
		if _, err := fmt.Fprintln(out, "Dry-run mode is enabled; Kubernetes will validate patches without persisting them. Use --dry-run=false to apply changes."); err != nil {
			return nil, err
		}
	}
	if !opts.yes {
		if opts.in == nil {
			opts.in = os.Stdin
		}
		action := "dry-run"
		if !opts.dryRun {
			action = "apply"
		}
		if _, err := fmt.Fprintf(out, "%s %d suggested patch(es) to HPA %s? [y/N]: ", action, len(patches), name); err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(opts.in)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return []string{"Apply skipped."}, nil
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer != "y" && answer != "yes" {
			return []string{"Apply skipped."}, nil
		}
	}

	var applied []string
	patchOptions := metav1.PatchOptions{}
	if opts.dryRun {
		patchOptions.DryRun = []string{metav1.DryRunAll}
	}
	for _, suggestion := range patches {
		_, err := client.Interface.AutoscalingV2().
			HorizontalPodAutoscalers(client.Namespace).
			Patch(ctx, name, types.MergePatchType, []byte(suggestion.Patch), patchOptions)
		if err != nil {
			return applied, fmt.Errorf("failed to apply suggested patch %q: %w", suggestion.Title, err)
		}
		if opts.dryRun {
			applied = append(applied, fmt.Sprintf("Dry-run validated: %s", suggestion.Title))
		} else {
			applied = append(applied, fmt.Sprintf("Applied: %s", suggestion.Title))
		}
	}
	return applied, nil
}

func patchDiff(currentMin *int32, currentDesired int32, currentMax int32, patch string) string {
	var parsed struct {
		Spec struct {
			MinReplicas *int32 `json:"minReplicas"`
			MaxReplicas *int32 `json:"maxReplicas"`
			Behavior    any    `json:"behavior"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(patch), &parsed); err != nil {
		return fmt.Sprintf("  patch: %s", patch)
	}
	lines := []string{fmt.Sprintf("  status.desiredReplicas: %d (current status, unchanged by patch)", currentDesired)}
	if parsed.Spec.MinReplicas != nil {
		current := int32(1)
		if currentMin != nil {
			current = *currentMin
		}
		lines = append(lines, fmt.Sprintf("  spec.minReplicas: %d -> %d", current, *parsed.Spec.MinReplicas))
	}
	if parsed.Spec.MaxReplicas != nil {
		lines = append(lines, fmt.Sprintf("  spec.maxReplicas: %d -> %d", currentMax, *parsed.Spec.MaxReplicas))
	}
	if parsed.Spec.Behavior != nil {
		lines = append(lines, "  spec.behavior: updated")
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("  patch: %s", patch))
	}
	return strings.Join(lines, "\n")
}

func reportHasCondition(report hpaanalysis.StatusReport, condition string) bool {
	want := normalizeSelector(condition)
	for _, current := range report.Analysis.Conditions {
		if normalizeSelector(current.Type) == want {
			return true
		}
	}
	return false
}

func normalizeSelector(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func shouldColorize(mode string, out io.Writer) bool {
	switch strings.ToLower(mode) {
	case "always", "true", "yes":
		return true
	case "never", "false", "no":
		return false
	case "", "auto":
		file, ok := out.(*os.File)
		return ok && term.IsTerminal(int(file.Fd()))
	default:
		return false
	}
}

func writeOutput(out io.Writer, format string, templateStr string, value any, writeText func() error) error {
	switch format {
	case "", "table", "wide", "ja":
		return writeText()
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "jsonpath":
		return writeJSONPath(out, templateStr, value)
	case "go-template", "template":
		return writeTemplate(out, templateStr, value)
	default:
		if expression, ok := strings.CutPrefix(format, "jsonpath="); ok {
			return writeJSONPath(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "jsonpath:"); ok {
			return writeJSONPath(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "template="); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "template:"); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "go-template="); ok {
			return writeTemplate(out, expression, value)
		}
		if expression, ok := strings.CutPrefix(format, "go-template:"); ok {
			return writeTemplate(out, expression, value)
		}
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func writeJSONPath(out io.Writer, expression string, value any) error {
	parser := jsonpath.New("output")
	parser.AllowMissingKeys(true)
	if err := parser.Parse(expression); err != nil {
		return fmt.Errorf("invalid jsonpath expression: %w", err)
	}
	if err := parser.Execute(out, value); err != nil {
		return fmt.Errorf("failed to execute jsonpath expression: %w", err)
	}
	_, err := fmt.Fprintln(out)
	return err
}

func writeTemplate(out io.Writer, expression string, value any) error {
	tmpl, err := template.New("output").Parse(expression)
	if err != nil {
		return fmt.Errorf("invalid template expression: %w", err)
	}
	if err := tmpl.Execute(out, value); err != nil {
		return fmt.Errorf("failed to execute template expression: %w", err)
	}
	_, err = fmt.Fprintln(out)
	return err
}

type eventOption struct {
	enabled bool
	limit   int
}

func (o *eventOption) Set(value string) error {
	switch value {
	case "", "true":
		o.enabled = true
		if o.limit <= 0 {
			o.limit = 5
		}
		return nil
	case "false":
		o.enabled = false
		return nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("events must be true, false, or a positive number")
	}
	if limit < 1 {
		return fmt.Errorf("events limit must be greater than zero")
	}
	o.enabled = true
	o.limit = limit
	return nil
}

func (o eventOption) String() string {
	if !o.enabled {
		return "false"
	}
	return strconv.Itoa(o.limit)
}

func (o eventOption) Type() string {
	return "boolOrInt"
}
