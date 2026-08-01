# valve in front of vLLM

Reuse [`examples/openai-proxy`](../openai-proxy) against a local [vLLM](https://docs.vllm.ai/) OpenAI-compatible server. No second proxy binary.

## 1. Start vLLM

```bash
# example — adjust model / GPU flags for your machine
vllm serve meta-llama/Meta-Llama-3-8B-Instruct --port 8000
```

OpenAI-compatible base URL: `http://127.0.0.1:8000/v1`

## 2. Start valve proxy

```bash
cd examples/openai-proxy
OPENAI_BASE_URL=http://127.0.0.1:8000/v1 \
  LISTEN=:8080 \
  RPM=60 \
  TPM=90000 \
  go run .
```

Optional Redis:

```bash
REDIS_ADDR=127.0.0.1:6379 OPENAI_BASE_URL=http://127.0.0.1:8000/v1 go run .
```

## 3. Call through valve

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"meta-llama/Meta-Llama-3-8B-Instruct","messages":[{"role":"user","content":"hi"}],"max_tokens":64}'
```

Denied requests return **429** with `x-ratelimit-*` headers from valve.

## Notes

- Point `OPENAI_BASE_URL` at any OpenAI-compatible upstream (vLLM, TGI, LiteLLM, etc.).
- For Anthropic-shaped ITPM/OTPM, use the Go library / `valved` with `input_tokens_per_minute` + `output_tokens_per_minute` (see [`docs/HTTP_API.md`](../../docs/HTTP_API.md)); the openai-proxy example remains classic TPM today.
