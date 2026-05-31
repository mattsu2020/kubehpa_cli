package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWriteOutputJSONPath(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
			Summary:   "HPA currently keeps the replica count unchanged.",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "jsonpath={.analysis.summary}", "", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "HPA currently keeps the replica count unchanged." {
		t.Fatalf("unexpected jsonpath output: %q", out.String())
	}

	// Test separate jsonpath format and template argument
	out.Reset()
	if err := writeOutput(&out, "jsonpath", "{.analysis.summary}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "HPA currently keeps the replica count unchanged." {
		t.Fatalf("unexpected jsonpath output: %q", out.String())
	}
}

func TestWriteOutputTemplate(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Namespace: "default",
			Name:      "web",
		},
	}

	var out bytes.Buffer
	if err := writeOutput(&out, "template={{ .Analysis.Namespace }}/{{ .Analysis.Name }}", "", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "default/web" {
		t.Fatalf("unexpected template output: %q", out.String())
	}

	// Test separate template format and template argument
	out.Reset()
	if err := writeOutput(&out, "go-template", "{{ .Analysis.Namespace }}/{{ .Analysis.Name }}", report, nil); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "default/web" {
		t.Fatalf("unexpected template output: %q", out.String())
	}
}

func TestMatchesListFilter(t *testing.T) {
	item := hpaanalysis.ListItem{
		Health: "LIMITED",
		Issue:  "LIMITED: TooManyReplicas",
	}

	for _, filter := range []string{"limited", "scaling-limited", "TooManyReplicas"} {
		if !matchesListFilter(item, filter) {
			t.Fatalf("expected filter %q to match %#v", filter, item)
		}
	}
	if matchesListFilter(item, "error") {
		t.Fatalf("did not expect error filter to match %#v", item)
	}
}

func TestMatchesHealthScoreThreshold(t *testing.T) {
	item := hpaanalysis.ListItem{HealthScore: 75}

	if !matchesHealthScoreThreshold(item, -1) {
		t.Fatal("expected disabled threshold to match")
	}
	if !matchesHealthScoreThreshold(item, 0) {
		t.Fatal("expected zero threshold to match as disabled")
	}
	if !matchesHealthScoreThreshold(item, 80) {
		t.Fatal("expected score 75 to match threshold 80")
	}
	if matchesHealthScoreThreshold(item, 60) {
		t.Fatal("did not expect score 75 to match threshold 60")
	}
	if !matchesHealthScoreThreshold(item, 120) {
		t.Fatal("expected threshold above 100 to be clamped and match")
	}
}

func TestSortListItemsByDesired(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Name: "api", Desired: 5},
		{Name: "web", Desired: 2},
	}

	sortListItems(items, "desired")
	if items[0].Name != "web" {
		t.Fatalf("expected web first, got %#v", items)
	}
}

func TestSortListItemsByDiff(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Name: "api", Current: 2, Desired: 2}, // diff = 0
		{Name: "web", Current: 3, Desired: 8}, // diff = 5
		{Name: "db", Current: 5, Desired: 2},  // diff = 3
	}

	sortListItems(items, "diff")
	if items[0].Name != "web" || items[1].Name != "db" || items[2].Name != "api" {
		t.Fatalf("expected order [web, db, api], got order: %s, %s, %s", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestSortListItemsProblemDefaultsWorstHealthFirst(t *testing.T) {
	items := []hpaanalysis.ListItem{
		{Namespace: "default", Name: "limited", HealthScore: 75, Current: 5, Desired: 5},
		{Namespace: "default", Name: "broken", HealthScore: 50, Current: 2, Desired: 2},
		{Namespace: "default", Name: "large-diff", HealthScore: 75, Current: 1, Desired: 8},
	}

	sortListItems(items, "problem")
	if items[0].Name != "broken" || items[1].Name != "large-diff" || items[2].Name != "limited" {
		t.Fatalf("expected [broken, large-diff, limited], got [%s, %s, %s]", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestSortListItemsByAge(t *testing.T) {
	now := metav1.Now()
	past := metav1.NewTime(now.Add(-10 * time.Minute))
	future := metav1.NewTime(now.Add(10 * time.Minute))

	items := []hpaanalysis.ListItem{
		{Name: "api", CreationTimestamp: now},
		{Name: "web", CreationTimestamp: future},
		{Name: "db", CreationTimestamp: past},
	}

	sortListItems(items, "age")
	if items[0].Name != "db" || items[1].Name != "api" || items[2].Name != "web" {
		t.Fatalf("expected order [db, api, web], got order: %s, %s, %s", items[0].Name, items[1].Name, items[2].Name)
	}
}

func TestPatchDiffIncludesCurrentDesiredReplicas(t *testing.T) {
	minReplicas := int32(2)
	diff := patchDiff(&minReplicas, 7, 10, `{"spec":{"maxReplicas":20}}`)
	if !strings.Contains(diff, "status.desiredReplicas: 7") {
		t.Fatalf("expected desiredReplicas context, got %q", diff)
	}
	if !strings.Contains(diff, "spec.maxReplicas: 10 -> 20") {
		t.Fatalf("expected maxReplicas diff, got %q", diff)
	}
}

func TestReportHasConditionNormalizesConditionName(t *testing.T) {
	report := hpaanalysis.StatusReport{
		Analysis: hpaanalysis.Analysis{
			Conditions: []hpaanalysis.Condition{{Type: "ScalingLimited"}},
		},
	}

	if !reportHasCondition(report, "scaling-limited") {
		t.Fatalf("expected scaling-limited to match ScalingLimited")
	}
}

func TestVersionCommandPrintsBuildMetadata(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"kubectl-hpa-status version", "commit=", "date="} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in version output, got %q", want, text)
		}
	}
}
