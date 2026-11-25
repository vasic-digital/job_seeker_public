# Backfilling body signatures (JSK-T193 follow-up)

Postings stored before the signature writer landed have no sealed MinHash signature (`posting.sig_sealed`), so the Resolver's `f_body_mh`
feature is absent for them until they are re-ingested. `jsctl backfill-signatures` closes that gap from the local database alone.

```
jsctl backfill-signatures              # dry run (default): counts, writes nothing
jsctl backfill-signatures --apply      # seals the missing signatures, one audit row (signature_backfill_applied)
```

- No network request is made; bodies are opened with the data key, signatures are sealed with it (never plaintext).
- Idempotent: a posting that already has a signature is never touched; a second `--apply` reports `pending=0` and writes no audit row.
- `empty_body` counts bodies with no words (left without a signature); `unreadable_body` counts bodies that do not open (left alone, never fatal).
- It does not re-run the Resolver. Run `jsctl resolve` afterwards if you want Positions re-evaluated with the new signatures.

Real-embedder check (opt-in, never part of the default suite):
`JSK_OLLAMA_LOOPBACK=http://127.0.0.1:11434 JSK_EVIDENCE_DIR=<dir> go test -v -run OllamaEmbedderLive ./internal/position`
(needs the `nomic-embed-text` model installed; the embedder refuses any non-loopback endpoint).
