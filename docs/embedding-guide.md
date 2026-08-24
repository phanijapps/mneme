# Embedding Provider Guide

mneme's write path computes embeddings through the `port.MemoryEncoder`
interface. The default backend is **BAAI/bge-small-en-v1.5** served by a local
**Ollama** instance (384 dimensions, cosine distance). This guide explains how
to swap that for another provider.

> Full origin context for the encoder lives in
> [docs/origin-story.md](origin-story.md).

---

## Architecture

```
service.MemoryService
        │
        ▼
port.MemoryEncoder            ← the seam
        │
        ├── adapter/encoder/bge      → Ollama /api/embed (default)
        ├── adapter/encoder/stub     → deterministic, offline (tests)
        └── adapter/encoder/<yours>  → any provider you add
        │
        ▼
adapter/encoder/encoder.go    ← factory: NewEncoder(Config{Provider, ...})
```

Adding a provider never touches the service layer: implement the three
methods of `port.MemoryEncoder`, register the provider string in
`NewEncoder`, done.

```go
// internal/port/encoder.go (excerpt)
type MemoryEncoder interface {
    Encode(ctx context.Context, content string, opts EncodeOptions) (EncodedMemory, error)
    EncodeBatch(ctx context.Context, contents []string, opts EncodeOptions) ([]EncodedMemory, error)
    ListModels(ctx context.Context) ([]*domain.EmbeddingModel, error)
}
```

---

## Current Default: BGE-small via Ollama

| | |
|---|---|
| **Model** | `bge-small-en-v1.5` (BAAI) |
| **Dimensions** | 384 |
| **Distance** | cosine |
| **Cost** | free — runs on your hardware |
| **Latency** | ~5–15 ms per text on CPU; sub-ms warm on GPU |
| **On-prem** | yes — no data leaves the machine |

Setup:

```bash
ollama pull bge-small-en-v1.5
export OLLAMA_BASE_URL=http://localhost:11434   # default if unset
```

The adapter (`internal/adapter/encoder/bge/encoder.go`) calls
`POST /api/embed` for vectors and `GET /api/tags` for `ListModels`, filtering
to embedding-capable model families. Connection failures surface as
`bge: ollama <url> unreachable: ...` at first use, not at construction.

---

## Swapping Providers

### a) AWS Bedrock — Amazon Titan Embeddings / Cohere Embed v3

| | |
|---|---|
| **Models** | `amazon.titan-embed-text-v2:0` (1024 dims), `cohere.embed-english-v3` (1024), `cohere.embed-multilingual-v3` (1024) |
| **Go module** | `github.com/aws/aws-sdk-go-v2/service/bedrockruntime` |
| **Cost** | ~$0.02 / 1M input tokens (Titan v2) |
| **When** | already on AWS; needs IAM-managed keys, no self-hosting |

**IAM requirements** — the principal needs:

```json
{
  "Effect": "Allow",
  "Action": ["bedrock:InvokeModel"],
  "Resource": "arn:aws:bedrock:*::foundation-model/amazon.titan-embed-text-v2:0"
}
```

**Adapter file** — `internal/adapter/encoder/bedrock/encoder.go`:

```go
package bedrock

import (
    "context"
    "encoding/json"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
    "github.com/phanijapps/mneme/internal/port"
)

type Encoder struct {
    client *bedrockruntime.Client
    model  string // e.g. "amazon.titan-embed-text-v2:0"
}

func New(ctx context.Context, region, model string) (*Encoder, error) {
    cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
    if err != nil {
        return nil, err
    }
    return &Encoder{client: bedrockruntime.NewFromConfig(cfg), model: model}, nil
}

func (e *Encoder) embed(ctx context.Context, texts []string) ([][]float32, error) {
    // Titan: {"inputText": "..."}; Cohere: {"texts": [...], "input_type": "search_document"}
    body, _ := json.Marshal(map[string]any{"inputText": texts[0], "dimensions": 1024, "normalize": true})
    out, err := e.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
        ModelId:     aws.String(e.model),
        ContentType: aws.String("application/json"),
        Body:        body,
    })
    if err != nil {
        return nil, err
    }
    var resp struct {
        Embedding []float32 `json:"embedding"`
    }
    if err := json.Unmarshal(out.Body, &resp); err != nil {
        return nil, err
    }
    return [][]float32{resp.Embedding}, nil
}

// Then implement Encode / EncodeBatch / ListModels on top of embed(),
// mirroring adapter/encoder/bge/encoder.go.
var _ port.MemoryEncoder = (*Encoder)(nil)
```

**Configuration** — standard AWS chain (`AWS_REGION`, `AWS_PROFILE`,
`~/.aws/credentials`, or `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`). Add
`ProviderBedrock = "bedrock"` to the factory switch in
`adapter/encoder/encoder.go`.

---

### b) Google Vertex AI — text-embedding-004

| | |
|---|---|
| **Models** | `text-embedding-004` (768 dims), `text-multilingual-embedding-002` (768) |
| **Go module** | `cloud.google.com/go/aiplatform` + `google.golang.org/api/option` |
| **Cost** | ~$0.02 / 1M input tokens |
| **When** | already on GCP; batch discounts at scale |

**Service account setup**:

```bash
gcloud iam service-accounts create mneme-encoder
gcloud projects add-iam-policy-binding "$PROJECT" \
  --member="serviceAccount:mneme-encoder@$PROJECT.iam.gserviceaccount.com" \
  --role="roles/aiplatform.user"
gcloud iam service-accounts keys create sa.json \
  --iam-account="mneme-encoder@$PROJECT.iam.gserviceaccount.com"
export GOOGLE_APPLICATION_CREDENTIALS=$PWD/sa.json
```

**Adapter file** — `internal/adapter/encoder/vertex/encoder.go`:

```go
package vertex

import (
    "context"

    aiplatform "cloud.google.com/go/aiplatform/apiv1"
    "cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
    "google.golang.org/api/option"
)

type Encoder struct {
    client  *aiplatform.PredictionClient
    endpoint string // projects/%s/locations/%s/publishers/google/models/text-embedding-004
}

func New(ctx context.Context, project, location string, opts ...option.ClientOption) (*Encoder, error) {
    client, err := aiplatform.NewPredictionClient(ctx, opts...)
    if err != nil {
        return nil, err
    }
    return &Encoder{
        client:  client,
        endpoint: fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s",
            project, location, "text-embedding-004"),
    }, nil
}

func (e *Encoder) embed(ctx context.Context, texts []string) ([][]float32, error) {
    instances := make([]*aiplatformpb.Value, len(texts))
    for i, t := range texts {
        instances[i] = &aiplatformpb.Value{
            Value: &structpb.Struct{Fields: map[string]*structpb.Value{
                "content": structpb.NewStringValue(t),
            }},
        }
    }
    resp, err := e.client.Predict(ctx, &aiplatformpb.PredictRequest{
        Endpoint: e.endpoint,
        Instances: instances,
        Parameters: &structpb.Value{StructValue: &structpb.Struct{Fields: map[string]*structpb.Value{
            "outputDimensionality": structpb.NewNumberValue(768),
        }}},
    })
    if err != nil {
        return nil, err
    }
    // each prediction holds values[].values — the float vector
    ...
}
```

**Configuration** — `GOOGLE_APPLICATION_CREDENTIALS` or Workload Identity
on GKE. Vector dims 768 — note this matches one of mneme's typed columns
(`vec_768`).

---

### c) MLflow — embeddings endpoint

| | |
|---|---|
| **Models** | whatever the tracking server exposes (typically `sentence-transformers/all-MiniLM-L6-v2`, 384 dims) |
| **Go module** | none — plain `net/http` |
| **Cost** | self-hosted compute |
| **When** | org already runs MLflow; want experiment-tracked embeddings |

**Auth setup** — MLflow 2.x supports basic auth:

```bash
export MLFLOW_TRACKING_URI=https://mlflow.internal:8443
export MLFLOW_TRACKING_USERNAME=mneme
export MLFLOW_TRACKING_PASSWORD=...
```

**Adapter file** — `internal/adapter/encoder/mlflow/encoder.go`:

```go
package mlflow

import (
    "bytes"
    "encoding/json"
    "net/http"
    "time"
)

type Encoder struct {
    base    string
    model   string
    client  *http.Client
}

func New(base, model string) *Encoder {
    return &Encoder{
        base:   base, // https://mlflow.internal:8443
        model:  model,
        client: &http.Client{Timeout: 30 * time.Second},
    }
}

func (e *Encoder) embed(texts []string) ([][]float64, error) {
    body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
    resp, err := e.client.Post(e.base+"/api/2.0/mlflow/embeddings", "application/json", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out struct {
        Embeddings [][]float64 `json:"embeddings"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    return out.Embeddings, nil
}
```

**Note** — the exact route depends on your MLflow deployment
(`/api/2.0/mlflow/embeddings` on recent servers; a served model endpoint via
`/invocations` on older ones). Check your server version before wiring.

---

### d) Ollama — other models

No new adapter needed. Change only the model tag in config:

```go
encoder.NewEncoder(encoder.Config{
    Provider: "ollama",
    Model:    "nomic-embed-text", // or "mxbai-embed-large", "bge-m3", ...
})
```

| Model | Dims | Notes |
|---|---|---|
| `bge-small-en-v1.5` | 384 | default; best size/quality for English |
| `bge-m3` | 1024 | multilingual, long context (8192) |
| `nomic-embed-text` | 768 | strong long-context English |
| `mxbai-embed-large` | 1024 | top MTEB scores at this size |
| `all-minilm` | 384 | fastest; lower ceiling |

The `bge` adapter already resolves dims per model tag and reports them via
`ListModels`.

---

### e) OpenAI — text-embedding-3

| | |
|---|---|
| **Models** | `text-embedding-3-small` (1536 dims), `text-embedding-3-large` (3072) |
| **Go module** | `github.com/openai/openai-go` (or plain `net/http` to `/v1/embeddings`) |
| **Cost** | $0.02 / 1M tokens (small); $0.13 / 1M (large) |
| **When** | easiest managed option; data leaves your network |

**Adapter file** — `internal/adapter/encoder/openai/encoder.go`:

```go
package openai

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "time"
)

const BaseURL = "https://api.openai.com/v1"

type Encoder struct {
    apiKey string
    model  string // "text-embedding-3-small" or "-large"
    client *http.Client
}

func New(apiKey, model string) *Encoder {
    return &Encoder{apiKey: apiKey, model: model, client: &http.Client{Timeout: 30 * time.Second}}
}

func (e *Encoder) embed(ctx context.Context, texts []string) ([][]float64, error) {
    body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
    req, _ := http.NewRequestWithContext(ctx, "POST", BaseURL+"/embeddings", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+e.apiKey)
    req.Header.Set("Content-Type", "application/json")
    resp, err := e.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var out struct {
        Data []struct {
            Embedding []float64 `json:"embedding"`
            Index     int       `json:"index"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, err
    }
    vecs := make([][]float64, len(out.Data))
    for _, d := range out.Data {
        vecs[d.Index] = d.Embedding // API may reorder; restore input order
    }
    return vecs, nil
}
```

**Configuration** — `OPENAI_API_KEY` env var; read it in the factory. Note
`text-embedding-3-small`'s 1536 dims match mneme's `vec_1536` typed column.

---

## Comparison

| Provider | Model | Dims | Cost / 1M tokens | Latency | On-prem |
|---|---|---|---|---|---|
| **Ollama** (default) | bge-small-en-v1.5 | 384 | free | ~5–15 ms CPU | ✅ |
| Ollama | bge-m3 | 1024 | free | ~15–40 ms CPU | ✅ |
| Ollama | nomic-embed-text | 768 | free | ~10–25 ms CPU | ✅ |
| Ollama | mxbai-embed-large | 1024 | free | ~20–50 ms CPU | ✅ |
| AWS Bedrock | titan-embed-text-v2 | 1024 | ~$0.02 | ~50–100 ms | ❌ |
| AWS Bedrock | cohere.embed-english-v3 | 1024 | ~$0.02 | ~50–100 ms | ❌ |
| Google Vertex | text-embedding-004 | 768 | ~$0.02 | ~30–80 ms | ❌ |
| MLflow (self-host) | all-MiniLM-L6-v2 | 384 | compute | hardware-dependent | ✅ |
| OpenAI | text-embedding-3-small | 1536 | $0.02 | ~50–150 ms | ❌ |
| OpenAI | text-embedding-3-large | 3072 | $0.13 | ~80–200 ms | ❌ |

## Dimension Support in the Schema

mneme's `memory_embeddings` table uses typed vector columns — pgvector HNSW
indexes require a fixed typmod per column. Currently declared:
**`vec_1536`** and **`vec_768`** (see `internal/domain/embedding_model.go`,
`SupportedDimensions`). bge-small's 384 dims are not among them; if you make
384 the production default, add a `vec_384 vector(384)` column + HNSW index
via a new goose migration and extend `SupportedDimensions`.

## Switching Models in Production

The `embedding_models` table tracks which model produced each vector. Two
rules follow:

1. **Never mix models in one column.** Vectors from different models are not
   comparable — a cosine similarity between a bge vector and an OpenAI vector
   is meaningless. Query paths must filter on `model_id`.
2. **Switching means re-embedding or dual-writing.** Options:
   - **Backfill**: iterate all memories, re-encode with the new model, insert
     into the new typed column, then flip `is_active` in `embedding_models`.
     Simple, offline, one-time cost.
   - **Multi-model search**: keep both models' vectors and fan hybrid search
     across both columns, fusing results. More code, no re-embed, higher
     query cost.
   - **Hybrid**: backfill historical content lazily — re-encode a memory the
     first time it is read after the switch.

`ListModels` exists so tooling can discover what is registered before
triggering either path.
