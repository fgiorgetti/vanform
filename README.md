# VanForm

## Description

The VanForm controller uses Vault to publish and consume credentials linking all participant sites to your VAN.

Sites can be configured to be part of a given VAN and to be placed in one or multiple zones.
A Site can also determine if a given zone it participates is reachable to other zones, within the same VAN.

If a given zone of your Site's configuration is reachable to another zone(s), a local token will be published
to all reachable zone(s).

Tokens available to your placed zones will be consumed and created locally, establishing your VAN.

The VanForm controller can run watching all namespaces on your cluster or watching a single namespace.

It also works with System Sites.

## Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: skupper-van-form
  namespace: west
data:
  config.json: |
    {
      "van": "hello-world",
      "url": "http://host.minikube.internal:8200",
      "path": "skupper",
      "secret": "skupper-van-form",
      "zones": [{
        "name": "west",
        "reachable_from": ["east"]
      }]
    }
```

- `van`: The name of your VAN (used to compose the path within Vault where tokens are published and consumed)
- `url`: Vault's URL
- `path`: The base KV2 path within Vault to place tokens (default: skupper)
- `secret`: Kubernetes secret name that contains vault credentials (default: skupper-van-form)
- `zones`: The zones in your VAN where the given site is placed. Each zone can be (optionally) configured to be `reachable_from` other zones within the same VAN.
 