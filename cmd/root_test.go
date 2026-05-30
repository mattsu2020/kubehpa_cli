package cmd

import (
	"bytes"
	"strings"
	"testing"

	hpaanalysis "github.com/mattsu2020/kubectl-hpa-status/pkg/hpa"
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
	if err := writeOutput(&out, "jsonpath={.analysis.summary}", report, nil); err != nil {
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
	if err := writeOutput(&out, "template={{ .Analysis.Namespace }}/{{ .Analysis.Name }}", report, nil); err != nil {
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
