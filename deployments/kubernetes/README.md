# Saltare Kubernetes Deployment

This directory contains Kubernetes manifests for deploying Saltare with Meilisearch (or Typesense) as the search backend.

## 📁 Files Overview

| File | Description |
|------|-------------|
| `namespace.yaml` | Saltare namespace |
| `secrets.yaml` | API keys and credentials |
| `configmap.yaml` | Saltare configuration |
| `saltare-deployment.yaml` | Saltare deployment + PVC |
| `saltare-service.yaml` | Saltare ClusterIP services |
| `meilisearch.yaml` | Meilisearch StatefulSet + Service |
| `typesense.yaml` | Typesense StatefulSet + Service (alternative) |
| `ingress.yaml` | Ingress configuration |
| `hpa.yaml` | HorizontalPodAutoscaler |
| `networkpolicy.yaml` | Network security policies |
| `kustomization.yaml` | Kustomize configuration |

## 🚀 Quick Start

### 1. Configure Secrets

Edit `secrets.yaml` and replace the base64-encoded values with your actual API keys:

```bash
# Encode your keys
echo -n "your-cerebras-api-key" | base64
echo -n "your-openrouter-api-key" | base64
echo -n "your-meilisearch-master-key" | base64
```

### 2. Deploy with Kustomize

```bash
# Preview what will be deployed
kubectl apply -k deployments/kubernetes/ --dry-run=client

# Deploy
kubectl apply -k deployments/kubernetes/

# Or using kustomize directly
kustomize build deployments/kubernetes/ | kubectl apply -f -
```

### 3. Deploy Manually (without Kustomize)

```bash
# Create namespace
kubectl apply -f namespace.yaml

# Create secrets
kubectl apply -f secrets.yaml

# Create configmap
kubectl apply -f configmap.yaml

# Deploy Meilisearch (or typesense.yaml)
kubectl apply -f meilisearch.yaml

# Deploy Saltare
kubectl apply -f saltare-deployment.yaml
kubectl apply -f saltare-service.yaml

# Optional: Ingress
kubectl apply -f ingress.yaml

# Optional: HPA
kubectl apply -f hpa.yaml

# Optional: Network Policies
kubectl apply -f networkpolicy.yaml
```

## 🔧 Configuration

### Switching Search Engines

To use **Typesense** instead of **Meilisearch**:

1. Edit `configmap.yaml`:
   ```yaml
   search:
     provider: typesense  # Change from "meilisearch"
   ```

2. Edit `kustomization.yaml`:
   ```yaml
   resources:
     # - meilisearch.yaml   # Comment out
     - typesense.yaml       # Uncomment
   ```

3. Redeploy:
   ```bash
   kubectl apply -k deployments/kubernetes/
   ```

### Enabling Hybrid Search (Meilisearch)

Meilisearch hybrid search requires the experimental vector store feature. This is already enabled in `meilisearch.yaml`:

```yaml
env:
  - name: MEILI_EXPERIMENTAL_VECTOR_STORE
    value: "true"
```

### Scaling

```bash
# Manual scaling
kubectl scale deployment saltare -n saltare --replicas=5

# Or use HPA (auto-scaling)
kubectl apply -f hpa.yaml
```

## 📊 Monitoring

### Check Status

```bash
# All resources
kubectl get all -n saltare

# Pods
kubectl get pods -n saltare -w

# Logs
kubectl logs -n saltare -l app.kubernetes.io/name=saltare -f

# Meilisearch logs
kubectl logs -n saltare -l app.kubernetes.io/name=meilisearch -f
```

### Health Checks

```bash
# Port forward for local access
kubectl port-forward -n saltare svc/saltare 8080:8080

# Test health endpoint
curl http://localhost:8080/api/v1/health

# Test MCP endpoint
kubectl port-forward -n saltare svc/saltare 8081:8081
curl -X POST http://localhost:8081/mcp -d '{"jsonrpc":"2.0","method":"initialize","id":1}'
```

## 🔒 Security

### Network Policies

The `networkpolicy.yaml` restricts traffic:
- Default deny all ingress
- Allow Saltare from ingress controller only
- Allow Saltare → Meilisearch/Typesense
- Allow Saltare → External HTTPS (LLM APIs)

### Secrets Management

For production, consider using:
- [External Secrets Operator](https://external-secrets.io/)
- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
- [Vault](https://www.vaultproject.io/)

Example with External Secrets:
```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: saltare-secrets
  namespace: saltare
spec:
  secretStoreRef:
    kind: ClusterSecretStore
    name: vault
  target:
    name: saltare-secrets
  data:
    - secretKey: cerebras-api-key
      remoteRef:
        key: saltare/cerebras
        property: api_key
```

## 🌐 Ingress

### With NGINX Ingress Controller

```bash
# Install NGINX Ingress
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/cloud/deploy.yaml

# Apply ingress
kubectl apply -f ingress.yaml
```

### With cert-manager (TLS)

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml

# Create ClusterIssuer
cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: your-email@example.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
      - http01:
          ingress:
            class: nginx
EOF

# Update ingress annotations
# cert-manager.io/cluster-issuer: letsencrypt-prod
```

## 📦 Production Checklist

- [ ] Replace placeholder API keys in `secrets.yaml`
- [ ] Update domain names in `ingress.yaml`
- [ ] Configure TLS certificates
- [ ] Set appropriate resource limits
- [ ] Enable Network Policies
- [ ] Configure HPA for auto-scaling
- [ ] Set up monitoring (Prometheus/Grafana)
- [ ] Configure log aggregation
- [ ] Set up backup for PersistentVolumes
- [ ] Test disaster recovery

## 🐳 Building Docker Image

```bash
# Build
docker build -t ghcr.io/denis-chistyakov/saltare:latest .

# Push
docker push ghcr.io/denis-chistyakov/saltare:latest

# Update deployment
kubectl set image deployment/saltare saltare=ghcr.io/denis-chistyakov/saltare:v1.0.0 -n saltare
```

## 🆘 Troubleshooting

### Pod not starting

```bash
# Check events
kubectl describe pod -n saltare -l app.kubernetes.io/name=saltare

# Check logs
kubectl logs -n saltare -l app.kubernetes.io/name=saltare --previous
```

### Meilisearch connection issues

```bash
# Check Meilisearch is running
kubectl get pods -n saltare -l app.kubernetes.io/name=meilisearch

# Test connectivity from Saltare pod
kubectl exec -n saltare -it deploy/saltare -- curl http://meilisearch:7700/health
```

### Storage issues

```bash
# Check PVC status
kubectl get pvc -n saltare

# Describe PVC
kubectl describe pvc saltare-data -n saltare
```

