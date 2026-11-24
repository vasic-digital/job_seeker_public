for r in http3 auth middleware ratelimiter I18n config database Storage cache observability recovery security challenges HelixQA containers LLMProvider discovery concurrency; do
  j=$(env -u GITHUB_TOKEN gh api repos/vasic-digital/$r 2>/dev/null) || { echo "$r UNREACHABLE"; continue; }
  sha=$(echo "$j" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["default_branch"],d["pushed_at"][:10],"priv="+str(d["private"]),"arch="+str(d["archived"]))')
  br=${sha%% *}
  head=$(env -u GITHUB_TOKEN gh api repos/vasic-digital/$r/commits/$br --jq .sha 2>/dev/null | cut -c1-12)
  gm=$(env -u GITHUB_TOKEN gh api repos/vasic-digital/$r/contents/go.mod --jq .content 2>/dev/null | base64 -d 2>/dev/null | sed -n '1p;3p' | tr '\n' ' ')
  echo "$r | $sha | $head | $gm"
done
