# i18n translation providers - survey and scheduler (2026-11-24)

All translation goes through the canonical umbrella pipeline (`_tools/translate-pipeline.sh`, HelixTranslate engine, section 11.4.255).
`scripts/i18n_schedule.py` rotates the pipeline's primary provider/model per call (the pipeline fallback is pinned to the same target),
keeps cooldown state in `$I18N_WORK/sched-state.json` and a per-key cache `$I18N_WORK/keys-<lang>.json`.
Reviewer: `scripts/i18n_review.sh <lang> [provider] [model]` (default cohere `command-r-plus-08-2024`; the review script refuses a reviewer provider equal to ANY translator provider recorded for that language, so translators are pinned per language with `--targets`, see "Per-language translator / reviewer selection").

## Survey (one tiny real request each, keys by env NAME only)

| Provider | Result | Note |
|---|---|---|
| groq | WORKS | per-model buckets: gpt-oss-120b (200000 tokens/day, 8000/min), gpt-oss-20b, qwen/qwen3.8-27b, allam-2-7b all translate through the engine |
| mistral | PARTIAL | `open-mistral-nemo` is limited to **2 requests/minute** (`x-ratelimit-limit-req-minute: 2`, measured 2026-11-24), so it is a REVIEWER only (the engine makes 1-2 calls per line); it translates in principle; medium/small/magistral answer 429 `rate_limited` (limit 0/min); large is 403 tier |
| cohere | **QUOTA-EXHAUSTED 2026-11-24** (trial key: 1000 API calls/month, HTTP 429, resets with the calendar month) - kept as OPTIONAL (`enabled:false`); was WORKS as translator AND reviewer | engine fixed 2026-11-24 (HelixTranslate PR #2: OpenAI-compat route, live model ids). Translator target `cohere-command-a-translate` (`command-a-translate-08-2025`, one real en->de OK; en->ar and en->ja spot-checked 2026-11-24, natural phrasing, better than nvidia on short UI words). Reviewer `command-r-plus-08-2024` via the umbrella reviewer. One provider cannot do both for the same language |
| codestral | WORKS (engine fixed) | HelixTranslate PR #2 adds the `codestral.mistral.ai/v1` route and skips the verification pass for explicit provider+model; real en->de OK. A code-specialised model: last in rotation, quality unreviewed |
| nvidia | WORKS (engine fixed) | `nvidia/riva-translate-4b-instruct-v2` (purpose-built translator) translates through the rebuilt image. The 401 "Invalid JWT" could NOT be reproduced with the same key (UNCONFIRMED origin); the real defect was discovery capping on dead models (404/410/422) so nothing verified. Needs `NVIDIA_API_KEY` in `~/.helixtranslate.env`. Other catalogue ids (gemma, starcoder) are not callable for this account |
| cerebras | needs credit | 402 payment required |
| deepseek | needs credit | 402 insufficient balance |
| huggingface | needs credit | 402 pre-paid credits |
| siliconflow, chutes, venice | need credit | 402 |
| openrouter | needs credit | free slug gone, paid slug 402 |
| upstage | suspended | 403 insufficient credit |
| fireworks | account suspended | 412 |
| gemini | key invalid | 400 (both key files) |
| github models | not a translator | returns HTTP 200 text/plain body `OK` for any prompt; the engine now refuses it with a typed error (HelixTranslate PR #3) instead of an unmarshal accident |
| sambanova, novita, kimi | key rejected | 401 |
| zhipu, zai | zhipu: model id invalid; zai: 429 overloaded | |
| hyperbolic, nvidia llama ids | decommissioned | 404 / 410 |
| together | no key configured | |
| local (ollama) | engine support added, not in rotation | HelixTranslate PR #4: opt-in `-provider ollama -model X`, loopback-only base URL. Only `qwen2.5:3b-instruct-q4_K_M` is installed (real run OK but ~110 s cold and not fit for 14 languages). From the container the host's 127.0.0.1 is not reachable (rootless podman), so it is for the host binary only |

## Spot-check matrix (2026-11-24, canonical pipeline, 6 short strings incl. `%d`/`%s` placeholders; OK = tags intact + placeholders in order + target script)

Probe harness: one pipeline call per provider/language. "ph" = placeholder verbs identical IN ORDER (the scheduler check is order-aware now, like `i18naudit`).

| Language | nvidia riva | groq qwen3.8-27b | groq gpt-oss-20b | groq gpt-oss-120b | codestral | mistral nemo |
|---|---|---|---|---|---|---|
| es fr zh ja | OK | OK | OK | OK | OK | 429 (2 req/min) |
| ko | OK | OK | no output | no output (TPD spent) | OK | 429 |
| ar hi | OK | OK | OK | no output | OK | - |
| fa | OK, Arabic-leaning wording | OK | OK | no output | placeholder dropped | - |
| tr | OK | OK | OK | no output | placeholder invented | - |
| sr (profile `sr_cyrl`) | **Latin output (unfit)** | **Cyrillic OK** | no output | - | **Cyrillic OK** | - |
| be | **Ukrainian output (unfit)** | Belarusian | Belarusian | - | Belarusian | - |
| kk | **Ukrainian/Russian output (unfit)** | Kazakh | Kazakh | - | Kazakh (weak grammar) | - |

Findings that shaped the plan: plain `--lang sr` makes the pipeline emit Serbian in LATIN script (it maps `sr` to the latin profile); the catalogue is Cyrillic, so the scheduler now calls the pipeline with `sr_cyrl` (`PIPE_LANG`). Several outputs for ja/ko/kk put the TOTAL before the COUNT for "Saved %d of %d" (word order differs); the order-aware placeholder check rejects those and the chunk is retried. Spot-checks are 6 strings, NOT a quality verdict; the independent review is.

## Quota arithmetic

The engine translates LINE BY LINE (one or two model calls per line, reasoning tokens included). 250 keys is on the order of
the whole 200000 tokens/day of one groq model, so one language per model per day is the realistic ceiling.

## Resume

    UMBRELLA=<umbrella checkout> I18N_WORK=<dir> python3 scripts/i18n_schedule.py --lang ru,sr,...   # resumable, exit 3 = quota exhausted
    python3 scripts/i18n_schedule.py --dry-run

## Per-language translator / reviewer plan (scripts/i18n_providers.json `lang_targets`, `lang_reviewer`)

| Language | Translator order (sticky, next on failure) | Reviewer (must differ from every translator provider) |
|---|---|---|
| es fr zh ja ko ar hi tr | nvidia riva, then codestral | groq `qwen/qwen3.8-27b` |
| fa | groq qwen3.8-27b, groq gpt-oss-20b, codestral | mistral `open-mistral-nemo` |
| sr (Cyrillic) be kk | groq qwen3.8-27b, groq gpt-oss-20b, codestral | mistral `open-mistral-nemo` |

Reviewer weakness, stated plainly (section 11.4.6): `open-mistral-nemo` is a 12B model reviewing Persian/Serbian/Belarusian/Kazakh - its PASS is weaker evidence than a PASS for the other languages, and codestral/mistral share a vendor (different provider names and keys, so the independence rule is met formally). A native-speaker review is not replaced by either. cohere `command-r-plus` is the better reviewer and returns when its monthly quota resets.
Rate awareness: groq models allow 1000 requests and 200000 tokens per day each and 7000-8000 tokens/minute; `day_keys: 240` per groq target caps the keys sent per UTC day, `--sleep-until-reset HOURS` makes the scheduler sleep (state saved) instead of exiting 3 when every target is capped.
Reviewer requests are SLICED (`scripts/i18n_review_chunked.py`, 25 keys per request): a whole-catalogue request (~9000+ tokens) is refused by every groq model (HTTP 413/429 on the per-minute cap), and `_tools/review_translation.py` now escalates `max_tokens` (1400, 2800, 5600, 8000; env `REVIEW_MAX_TOKENS`, `REVIEW_MAX_TOKENS_CAP`) because reasoning models spend the budget on hidden reasoning and returned "no parseable verdict".

## Older per-language selection notes

Rotation order in `scripts/i18n_providers.json`: cohere `command-a-translate`, nvidia `riva-translate-4b-instruct-v2`, groq (gpt-oss-120b, gpt-oss-20b, qwen3.8-27b), mistral-nemo, codestral LAST (code-specialised, quality unreviewed).
Rule (section 11.4.256): the reviewer provider must differ from every translator provider of that language. Pin the translator set of a language with `i18n_schedule.py --lang <l> --targets <id,...>` and review with a provider outside that set:

| Translator set for a language | Reviewer |
|---|---|
| `cohere-command-a-translate` only | groq `openai/gpt-oss-120b` (or mistral `open-mistral-nemo`) |
| `nvidia-riva-translate` and/or groq/mistral targets | cohere `command-r-plus-08-2024` |

The translator actually used per language is recorded in `evidence/i18n/<lang>/pipeline.json` and the reviewer in `verdict.json`.
Landing: `scripts/i18n_review.sh` normalises its own evidence output (`scripts/evnorm.sh -w`), so `review.json` never carries a host path; after any hand-written or other-script evidence under `evidence/i18n/<lang>/` run `bash scripts/evnorm.sh -w evidence/i18n/<lang>` and confirm `bash gates/verify_evidence_normalised.sh`. Guard: `gates/verify_i18n_review_normalised.sh`.
nvidia is not an umbrella reviewer provider (not in `_tools/review_translation.py PROVIDERS`).
