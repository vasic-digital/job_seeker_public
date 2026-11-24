# Security and Privacy Checklist: job_seeker MVP

Each item is a REQUIREMENT-quality check (is it specified, measurable, owned), not an implementation test.

- [x] CHK001 Personal data placement is specified per location (spec FR-054..FR-056, data-model "Privacy rule", overlay contract)
- [x] CHK002 A proof exists or is planned that a gate can fail (needle controls: JSK-T019, JSK-T088; SC-010)
- [x] CHK003 Secrets handling states env NAMES only and file modes (FR-053, FR-050)
- [x] CHK004 Authentication threat model items covered: CSRF, rebinding, brute force, step-up, recovery (FR-044..FR-048, quickstart section 4)
- [x] CHK005 SSRF and injection surfaces specified (FR-009, FR-012, FR-049, SC-013)
- [x] CHK006 Outbound egress is one client with allow-list (FR-009)
- [x] CHK007 No unattended submit is a testable property (FR-027, SC-005)
- [x] CHK008 Hosted decision layer data minimisation is specified (FR-060)
- [x] CHK009 Retention, export, delete, backup horizon specified (FR-006, FR-058, FR-059)
- [x] CHK010 Log redaction has a canary acceptance (FR-051, SC-010)
- [x] CHK011 Supply-chain pinning specified (FR-052)
- [ ] CHK012 Provider data-retention/training terms of the hosted decision endpoint: UNKNOWN, must be checked before real posting text is sent (owner: JSK-T106 live tiebreak task precondition; operator Open question Q7)
- [ ] CHK013 Legal posture of jobops licence: no legal review exists (owner: JSK-T096 memo; operator decision)
