# Quickstart (operator) — job_seeker MVP

Written before implementation as the acceptance walkthrough; commands are the intended shape and MUST be re-verified against the built artefact (anything not run is UNCONFIRMED until its task evidence exists).

## 0. Prerequisites

Go 1.26.x, rootless podman, a trusted certificate for the console host name (private CA or tailnet certificate; QUIC will not negotiate on an untrusted certificate), the private overlay checkout at `CAREER_PRIVATE_DIR`, the unsealed template PDFs and manifest, and the secrets exported by NAME from your own shell profile (never typed into a file in this repo).

## 1. First run

```bash
export JOBSEEKER_ENV=production            # boot guards on
# DB key, session key, bootstrap token come from your secret store by name:
#   JOBSEEKER_DB_KEY  JOBSEEKER_SESSION_KEY  JOBSEEKER_BOOTSTRAP_TOKEN
jsctl migrate                              # takes a hardlinked backup first; refuses if that fails
jobseekerd                                 # prints the capability matrix (names only); exits non-zero on a violated invariant
```

Open the console, register the first passkey with the bootstrap token (valid only while zero passkeys exist), register a SECOND authenticator immediately, and store the recovery token offline.

## 2. The journey (dry-run)

1. Settings: enable Remotive, RemoteOK, Himalayas; note the shown next-allowed times (limits are enforced).
2. Refresh: postings appear with provider attribution links.
3. Inbox: ranked by score; open "Why" for explainability; Review queue holds `unmapped` and `ambiguous` items.
4. Shortlist, Prepare: the CV and cover template of slot (area, language) appear with SHA-256.
5. Approve (dry-run): state `dry_run_recorded`, nothing is sent anywhere.
6. Live: enable live mode in Settings (resets on restart), approve with typed company name and passkey step-up, open the provider apply URL, finish there, record the confirmation reference: state `sent`.
7. Tracker: record replies, interviews, outcomes, follow-ups.

## 3. Safety nets

Kill switch in Settings or `JOBSEEKER_KILL_SWITCH=1`; `jsctl verify-audit` recomputes the chain; `jsctl export` / `jsctl delete` per FR-058; `jsctl backup` and a restore drill per FR-059.

## 4. Lost passkeys

Use the second authenticator. If both are lost: on the host, set a fresh `JOBSEEKER_BOOTSTRAP_TOKEN`, run `jsctl passkeys reset` (writes a tombstone and audit entry), re-register. The host owner is the root of trust.

## 5. Verification entry points

`make gates` (static and live gates), `make test-all` (13 types, honest SKIPs listed), `make full` (one-command deterministic dry-run journey), `gates/prove-*.sh` (each proves its gate can fail).
