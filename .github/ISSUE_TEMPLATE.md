## Type

- [ ] Bug report
- [ ] Feature request
- [ ] Documentation improvement
- [ ] Question

## What happened?

## What did you expect to happen?

## Commands

```sh

```

## Output

```text

```

## Environment

- kubectl-hpa-status version:
- install method: Krew / Homebrew / GitHub release / source
- kubectl version:
- Kubernetes server version:
- HPA API version:
- metrics provider: metrics-server / custom metrics / external metrics / unknown

## HPA context

Please redact sensitive names if needed.

```sh
kubectl get hpa <name> -n <namespace> -o yaml
kubectl describe hpa <name> -n <namespace>
```

## Notes

For scaling explanations, this project only claims what is visible from HPA
status, metrics, behavior, and Events. If the API does not expose a controller
decision directly, include any ambiguity you noticed.
