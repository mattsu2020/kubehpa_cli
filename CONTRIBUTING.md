# Contributing

Thanks for helping improve `kubectl-hpa-status`.

## Development

```sh
go mod tidy
go test ./...
go build -o kubectl-hpa-status .
```

Run the plugin locally:

```sh
./kubectl-hpa-status status <hpa-name> -n <namespace>
./kubectl-hpa-status list -A
```

## Commit style

Use Conventional Commits where practical:

```text
feat: add hpa list command
fix: avoid treating inactive desiredReplicas as scale down
test: cover tolerance-like no-scale interpretation
```

## Pull requests

Include:

- the user-visible behavior changed
- how it was tested
- any remaining HPA API ambiguity the implementation intentionally avoids claiming
