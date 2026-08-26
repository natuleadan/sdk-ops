# yuga-cluster

YugabyteDB deployed **inside a k3s cluster** via the `yugabyte-k8s-operator`
(helm). This is the `mode: k3s` variant of yugabyte — RF=3, **no host ports**:
the microservices running in the same k3s cluster consume it over internal
service DNS (`yb-master.<ns>` / `yb-tserver.<ns>`). The same role CloudNativePG
plays for postgres.

## Deploy

```yaml
mode: k3s
hosts:
  - name: node-01
    ssh_key: /path/key
    user: root
    services:
      yuga:
        profile: lite
```

Then from the operator machine (kubectl pointed at the k3s cluster):

```bash
bash deploy.sh   # helm install the yugabyte-k8s-operator (RF=3)
bash validate.sh # statefulsets ready + YSQL reachable
```

## Env

| Var | Default | Meaning |
|---|---|---|
| `YB_K8S_NAMESPACE` | `yb-demo` | K8s namespace |
| `YB_K8S_RELEASE` | `yb-demo` | Helm release name |
| `YB_TAG` | `2026.1.1.1-b2` | Image tag |
| `YB_MASTER_REPLICAS` | `3` | Master replicas (RF=3) |
| `YB_TSERVER_REPLICAS` | `3` | TServer replicas |

## Gotchas

- No host ports: the cluster is consumed over internal service DNS.
- RF=3 needs 3 worker nodes (or 3 schedulable nodes) for true quorum.
- The operator (helm) handles the YBManaged CRD declaratively.
