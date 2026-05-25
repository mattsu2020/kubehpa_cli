# kubectl-hpa-status

POC for inspecting an HPA with the existing Kubernetes API:

```sh
kubectl hpa status <hpa-name> -n <namespace>
```

The plugin reads:

- `autoscaling/v2` `HorizontalPodAutoscaler`
- `status.currentReplicas`
- `status.desiredReplicas`
- `status.currentMetrics`
- `status.conditions`
- recent HPA Events

It intentionally does not reimplement the HPA controller's internal decision logic.

## Build

```sh
go mod tidy
go build -o kubectl-hpa-status .
```

To make it visible to `kubectl plugin list`, put the binary on `PATH`.

```sh
chmod +x ./kubectl-hpa-status
sudo mv ./kubectl-hpa-status /usr/local/bin/
kubectl plugin list
```

## Validation matrix

| Case | Explainable with existing signals? | Signals used | API gap |
| --- | --- | --- | --- |
| CPU above target and scale up | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Small |
| CPU below target and scale down | Mostly yes | `currentMetrics`, `desiredReplicas`, Events | Small |
| Limited by `maxReplicas` | Yes | `ScalingLimited`, `maxReplicas` | Small |
| Metrics fetch failure | Yes | `ScalingActive=False`, Events | Small |
| Multiple metrics and final winner | Partially hard | `currentMetrics`, `spec.metrics` | Medium |
| Stabilization or tolerance suppresses scale | Conditionally hard | conditions, Events | Medium to large |
| Missing metrics or not-ready pods affect decision | Hard | insufficient existing status | Large |

## Local validation notes

Validated on a local kind cluster named `hpa-status-poc`.

| Case | Observed result |
| --- | --- |
| metrics-server absent | `ScalingActive=False`, `FailedGetResourceMetric`, and HPA Events explained the failure. `desiredReplicas=0` was not treated as a scale-down recommendation. |
| metrics-server present, CPU below target | `ScalingActive=True`, `currentMetrics` showed CPU below target, and desired replicas stayed unchanged. |
| CPU above target | HPA emitted `SuccessfulRescale` and scaled the deployment to `maxReplicas`. |
| maxReplicas limit | `ScalingLimited=True`, `TooManyReplicas`, and `desiredReplicas == maxReplicas` were enough to explain the visible cap. |
| scale-down stabilization | After load stopped, CPU dropped below target while `AbleToScale=True` with `ScaleDownStabilized`; the plugin could explain the visible condition but not the controller's internal recommendation history. |

## Limitation

This plugin reports what can be inferred from existing HPA status, metrics,
conditions, and events. It does not know the controller's internal intermediate
calculations.
