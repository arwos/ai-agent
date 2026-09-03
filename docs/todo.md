# TODO

## Optional semantic search with embeddings

Current knowledge-base, memory, and skills search is based on Bleve/BM25 and
is sufficient for exact terms, file names, code, tags, and documentation.

Do not add a vector database yet. Revisit this task only after real examples
show that relevant records are missed because the user uses different wording
(for example, a request about configuring a model does not find an
OpenAI-compatible provider configuration document).

If needed, implement hybrid retrieval:

1. Generate embeddings with the local Ollama model `embeddinggemma:300m`.
2. Keep the existing Bleve/BM25 search for lexical relevance.
3. Store persistent profile-scoped vector indexes next to file-backed data.
4. Combine lexical and semantic results with weighted or reciprocal-rank
   fusion.
5. Rebuild embeddings when source documents, notes, or skills change.

For an embedded pure-Go implementation, evaluate `github.com/coder/hnsw`
before adding any new dependency. Avoid CGO-backed vector indexes unless a
measured performance requirement justifies the additional cross-platform build
complexity.
