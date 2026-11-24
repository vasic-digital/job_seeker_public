# pkg/ trees for the verdict rows, then exported symbols of the packages whose API was read.
for r in http3 auth middleware ratelimiter I18n config database security observability recovery; do
  echo "=== pkg tree: $r"
  env -u GITHUB_TOKEN gh api "repos/vasic-digital/$r/git/trees/HEAD?recursive=1" --jq '[.tree[]|select(.type=="tree")|.path]|map(select(test("^(pkg|internal)/[^/]+$")))|join(" ")'
done
for p in "http3 pkg/server" "middleware pkg/brotli" "middleware pkg/altsvc"; do set -- $p; echo "=== symbols: $1/$2"
  for f in $(env -u GITHUB_TOKEN gh api repos/vasic-digital/$1/contents/$2 --jq '.[]|select(.name|endswith(".go") and (endswith("_test.go")|not))|.name'); do
    echo "-- $f"; env -u GITHUB_TOKEN gh api repos/vasic-digital/$1/contents/$2/$f --jq .content | base64 -d | grep -E "^(func|type) |^// Package" | cut -c1-150 | head -20
  done
done
echo "=== negative checks (case-insensitive grep over http3/pkg/server/server.go and middleware/pkg/brotli/brotli.go)"
for p in "http3 pkg/server/server.go" "middleware pkg/brotli/brotli.go"; do set -- $p
  env -u GITHUB_TOKEN gh api repos/vasic-digital/$1/contents/$2 --jq .content | base64 -d > /tmp/.js_probe_src 2>/dev/null
  for pat in "alt-svc" "0rtt\|0-rtt\|allow0rtt" "q=" "etag" "range\|206" "tls.Listen\|ListenAndServeTLS"; do printf '%s %s : %s hit(s)\n' "$2" "$pat" "$(grep -ci "$pat" /tmp/.js_probe_src)"; done
done
