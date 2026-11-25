// glue.cjs - detects an e-mail address glued to the following word, the signature of captured table output where the
// cell separators were lost (Locator.allTextContents() on a <tr> concatenates cells with no separator). Shared by
// journey.cjs (fail at capture time) and gates/verify_evidence_glue.sh (scan stored evidence).
const TLD = '(?:com|org|net|io|ru|digital|dev|app|info|edu|gov|local|lan|test|invalid)';
const GLUED = new RegExp('[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)*?\\.' + TLD + '(?=[A-Za-z0-9])', 'g');
function gluedEmails(s) { return String(s).match(GLUED) || []; }
module.exports = { gluedEmails };
if (require.main === module) {
  const fs = require('fs'); let bad = 0;
  for (const f of process.argv.slice(2)) {
    fs.readFileSync(f, 'utf8').split('\n').forEach((l, i) => { for (const m of gluedEmails(l)) { console.log(f + ':' + (i + 1) + ': e-mail glued to following text: ' + m); bad++; } });
  }
  process.exit(bad ? 1 : 0);
}
