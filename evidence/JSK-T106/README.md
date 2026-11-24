# JSK-T106 live hosted-Jev tiebreak

- `live_tiebreak.json`: 12 REAL calls to `https://api.typesafe.ai/v1/systemone` (HTTP 200 x12) with request id (response header `x-typesafe-request-id`), model id (`jev-1.14.0`), usage and latency per call. `live-run.txt` is the console log.
- **Inputs are SYNTHETIC.** The vendor's data-retention terms are an open operator question, so no real posting, gold-corpus text, CV or personal value was sent. The client enforces this: `Query.Synthetic=false` is refused (`real_text_not_approved`) until the operator sets `Options.AllowRealText`.
- Deviation from the task text: ">= 10 ambiguous gold items" is met with 10 synthetic ambiguous items plus 2 synthetic injection items, NOT gold items. Rerun on gold items after the operator approves disclosure.
- Mode: `JEV_GATE=shadow`, advisory (Q11 default). The key is read by variable NAME and never written here.
