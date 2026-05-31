# Architecture

`kubectl-hpa-status` is a kubectl plugin that turns visible
`autoscaling/v2` HorizontalPodAutoscaler signals into operator-focused status,
health, and safe next-action suggestions.

## Boundaries

The tool intentionally does not reimplement the HPA controller. It reads only
stable Kubernetes API surfaces:

- HPA spec and status
- HPA conditions
- current metric status
- `spec.behavior`
- recent Events

When Kubernetes does not expose an internal decision, the output must say so.
Inference should be labeled with confidence language and covered by tests.

## Package Layout

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra commands, flags, Kubernetes client orchestration, output format routing |
| `internal/kube/` | kubeconfig resolution, client construction, test helpers |
| `internal/style/` | terminal color and semantic styling |
| `pkg/hpa/analysis.go` | HPA signal extraction, summaries, interpretation, health scoring |
| `pkg/hpa/suggestions.go` | safe patch suggestion generation |
| `pkg/hpa/text.go` | human-readable status, list, and watch output |
| `pkg/hpa/events.go` | recent Event lookup and formatting |
| `test/e2e/` | kind-backed command path tests |

`pkg/hpa` is kept importable so downstream tools can reuse the analysis model
without depending on Cobra command wiring.

## Analysis Flow

1. `cmd` loads one or more HPAs through the Kubernetes client.
2. `pkg/hpa.Analyze` converts raw HPA objects into a structured `Analysis`.
3. Conditions, metrics, behavior, health, interpretation, and suggestions are
   attached to the same model.
4. Output writers render text, JSON, YAML, JSONPath, or templates.

## Suggestion Safety

Patch suggestions are conservative:

- Suggestions require visible HPA status evidence.
- Copy-paste commands include `--dry-run=server`.
- `--apply` defaults to server-side dry-run.
- Persistent changes require `--dry-run=false`.
- maxReplicas suggestions include preconditions and capacity warnings.

## Future Design Notes

Kubernetes may eventually expose structured HPA scaling decisions. If that API
arrives, add it as another input signal rather than replacing the existing
analysis model. The current boundary should remain: use explicit controller
signals when available, and clearly label inference when they are not.

Concrete integration plan:

- Add a small adapter that converts the new structured decision fields into the
  existing `Analysis` model instead of leaking raw API shape into renderers.
- Prefer structured controller reasons over current best-effort inference for
  metric winner, tolerance, missing-metric handling, and stabilization history.
- Keep the old inference path as a fallback for older clusters and mark it with
  lower confidence when structured decisions are absent.
- Extend JSON/YAML output with additive fields only; do not rename existing
  fields such as `summary`, `conditions`, `metrics`, or `suggestions`.
- Add fixture tests that compare the same HPA with and without structured
  decision data so behavior remains compatible across Kubernetes versions.
