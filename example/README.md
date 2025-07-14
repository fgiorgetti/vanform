# Start Vault Server in Dev mode

```bash
vault server -dev
```

_Note:_ Do not use a dev server in production.

## Set the Vault credentials

Set the VAULT_ADDR and VAULT_TOKEN environment variables, based on the values
returned when you started vault. Example:

```bash
export VAULT_ADDR='http://127.0.0.1:8200'
export VAULT_TOKEN='your-root-token-goes-here'
```

# Create a role-id to access Vault

## Enable approle authentication method

```bash
vault auth enable approle
```

## Create a vault policy

```bash
cat << EOF > /tmp/skupper-policy.hcl
path "skupper/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
EOF
vault policy write skupper-policy /tmp/skupper-policy.hcl
```

_**Note: **_ This policy is generic and not suitable for production use.
   Make sure that in production your policies are defined with the correct permissions based on each region visibility.

## Create a role-id associated with the skupper policy

```bash
vault write -format=json -f auth/approle/role/skupper \
    token_policies="skupper-policy"
```

## Create a secret-id

```bash
vault write -format=json -f auth/approle/role/skupper/secret-id | tee /tmp/secret-id.json
```

## Save credentials to Kubernetes secret

```bash
role_id=$(vault read -format=json auth/approle/role/skupper/role-id | jq -r .data.role_id)
secret_id=$(jq -r .data.secret_id < /tmp/secret-id.json)
kubectl create secret generic skupper-van-form \
    --dry-run=client \
    --output=yaml \
    --from-literal=role-id=${role_id} \
    --from-literal=secret-id=${secret_id} | tee /tmp/vault-secret.yaml
```

The `skupper-van-form` Secret is used to configure the credentials used by VanForm to
authenticate your site information against Vault. You will need one per participating
Site/Namespace.

In this example, we will use two. One for the `west` and another for the `east` namespace.

# Enable the base path to be used by VanForm

```bash
vault secrets enable -path=skupper -version=2 kv
```

By default, VanForm will try to use `/skupper` as the base path for sharing
credentials between sites.

You can use a custom path if desired.

# Configure your site definition

Each participating Site/Namespace must provide a ConfigMap named `skupper-van-form`.
Below, you can find the two ConfigMaps needed to run this example:

Save the YAML below to /tmp/west-configmap.yaml.

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

Save the YAML below to /tmp/east-configmap.yaml.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: skupper-van-form
  namespace: east
data:
  config.json: |
    {
      "van": "hello-world",
      "url": "http://host.minikube.internal:8200",
      "path": "skupper",
      "secret": "skupper-van-form",
      "zones": [{
        "name": "east"
      }]
    }
```

The structure of the VanForm configuration is defined in the README.md file at the repository's root.

# Deploying VanForm

You can deploy `VanForm` to run at cluster scope, watching all namespaces in your cluster.
It can also be deployed to watch a particular namespace. There are two sample deployment files that can be used.

To deploy it at cluster scope, run:

```bash
kubectl create namespace skupper || true
kubectl apply -f deployments/vanform-cluster-scope.yaml
```

It deploys the `VanForm` controller on the `skupper` namespace.
Make sure the namespace already exists before deploying it.

Assert that the skupper-van-former pod is running.

```bash
kubectl -n skupper get deploy/skupper-vanform
```

You should see an output similar to:

```
NAME              READY   UP-TO-DATE   AVAILABLE   AGE
skupper-vanform   1/1     1            1           59s
```

# Create your sites

Now that `VanForm` is running, let's create our sites on west and east namespaces.

## Create the west site

```bash
$ kubectl create namespace west
namespace/west created

$ skupper -n west site create west --enable-link-access
Waiting for status...
Site "west" is ready.
```

## Create the east site

```bash
$ kubectl create namespace east
namespace/east created

$ skupper -n east site create east
Waiting for status...
Site "east" is ready.
```

## Verify your sites are ready

```bash
kubectl -n west get site
kubectl -n east get site
```

# Configure VanForm

Now that your sites are ready, it is time to configure `VanForm`.
All we have to do is apply the ConfigMaps and Secret to `west` and `east` namespaces.

```bash
kubectl -n west apply -f /tmp/vault-secret.yaml
kubectl -n west apply -f /tmp/west-configmap.yaml

kubectl -n east apply -f /tmp/vault-secret.yaml
kubectl -n east apply -f /tmp/east-configmap.yaml
```

## Verify your sites are connected

```bash
kubectl -n west get site
kubectl -n east get site
```

And you should see something like:

```
NAME   STATUS   SITES IN NETWORK   MESSAGE
west   Ready    2                  OK
NAME   STATUS   SITES IN NETWORK   MESSAGE
east   Ready    2                  OK
```

# Cleaning up

- Remove the west and east namespaces:

```bash
kubectl delete namespaces west east
```

- Delete VanForm from the skupper namespace

```bash
kubectl delete -f deployments/vanform-cluster-scope.yaml 
```

- Stop the vault server
