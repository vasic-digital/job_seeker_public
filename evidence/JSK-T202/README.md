# JSK-T202 Bundesagentur Jobsuche adapter (PARTIAL, BLOCKED live, behind operator flag)
- red.txt RED first; green.txt GREEN under -race; FuzzBundesagenturOne 15 s no findings; mutation pairs jsk016-ba-gated / -ref-shape / -https-apply KILLED.
- Live: ONE read, GET rest.arbeitsagentur.de/jobboerse/jobsuche-service/pc/v4/jobs?was=Softwareentwickler&angebotsart=1&size=10&page=1 with the community-documented X-API-Key: HTTP 403 (ip 195.88.116.243, TLS rest.arbeitsagentur.de; testdata/ba_jobs.provenance.json). No second read. So the key and the response shape are UNCONFIRMED; no payload was captured.
- Fixture testdata/ba_memoryderived.json is MEMORY-DERIVED (labelled), input to the translator only.
- Inert by construction: Fetch refuses (ClassBlocked) before any request unless NewBundesagentur(true) AND a key is supplied; the operator flag/key config wiring (JOBSEEKER_BA_OK, JOBSEEKER_BA_API_KEY) is NOT done here (internal/app is another agent's area). The official terms page (arbeitsagentur.de/en/terms-of-use) was not read: re-check before enabling.
- Task NOT ticked.
