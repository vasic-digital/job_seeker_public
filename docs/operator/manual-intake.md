# Manual intake: what is refused, and the limit of a syntactic check (JSK-T200)

`jsctl intake --url U --title T [--company C] [--text-stdin]` stores one operator-pasted posting under provider `manual:<host>`. It makes **no network
request**. A refused input is exit code 2 and stores nothing (not even a provider row).

Refused: any scheme but https; user info; a port; a fragment; an IP literal or shorthand IPv4 form; a single-label name; a trailing dot; an empty,
over-long (>63) or hyphen-edged label (`a..com`, `-a.example.com`); a non-ASCII or over-57-character host; a numeric-only last label; and a host under a
reserved or private-use suffix:

| Suffix | Source |
|---|---|
| `.localhost` `.invalid` `.test` `.example` | RFC 6761 |
| `.local` | RFC 6762 (mDNS) |
| `.home.arpa` | RFC 8375 |
| `.arpa` as a whole (reverse zones) | RFC 1035, RFC 3596 |
| `.onion` | RFC 7686 |
| `.alt` | RFC 9476 |
| `.internal` | ICANN private-use reservation |
| `.lan` `.home` `.corp` `.localdomain` `.intranet` `.private` | de-facto suffixes of home and office networks |

`example.com`, `example.org` and `example.net` stay accepted (they are second-level names and the test fixtures use them); so does any name that merely
*contains* a reserved word (`local.example.com`, `contest.com`).

## What a syntactic check cannot catch

A public-looking name can resolve to loopback or a private address (`127.0.0.1.nip.io`, or any domain whose A record the attacker controls). No string
rule detects that. Today it is harmless: intake never fetches. **Any later code path that fetches a manual host** (a single-job ATS read, a link check,
a read-through) must: resolve the name itself; check every returned address against the egress deny-list (`internal/egress`); connect to that validated
address (pin it, no second lookup); and repeat the check on every redirect hop. The single-job ATS read (`jsctl read-job --url U [--store]`, JSK-T200) is that path and it does exactly this: only a Greenhouse, Lever or Ashby posting link resolves (a manual host never does), the request goes to the fixed allow-listed API host through `internal/egress` (allow-list before DNS, one lookup, every address checked against the deny-list, connection pinned to the validated address, redirects limited to the same host and re-checked per hop), robots at the exact URL and the provider's terms acceptance are asked first, and the default is a dry run that stores nothing. Evidence: `evidence/JSK-T200d`.

The refusal table is `internal/providers/testdata/manual_intake.tsv`; `bash gates/verify_manual_intake.sh [--prove-failure]` runs it through the library
and the real `jsctl intake` entry.
