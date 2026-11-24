package match

import (
	"math"
	"strings"
	"unicode"
)

// Language detection (FR-021, JSK-T053): a deterministic script + stop-word detector. Pure function of the text,
// no model, no network. It returns a language code and a confidence in [0,1]; callers store the language only
// when the confidence is >= LangMinConfidence and otherwise store "und" (never a guess).
//
// Measured status (see evidence/JSK-T053): calibrated and measured on real English postings. RU, SR and DE
// branches are implemented from script and stop-word tables but have NOT been measured on real postings
// because none exist in the ingest corpus; those cells stay operator-supplied (T048 availability baseline).

const LangMinConfidence = 0.8

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var langStops = map[string]map[string]bool{
	"en": wordSet("the and for with you our are will your have this that from to of in a an is we team work experience be as on or it at by about their they who what can all not more able including strong skills ability join us role"),
	"de": wordSet("der die das und für mit sie wir ihre ihr ist sind eine ein einen einer von zu im den dem des auf bei als oder auch nicht werden haben unser unsere dich du aus nach über wenn"),
	"sr": wordSet("i je za sa na se u koji koja koje smo ili kao od do biti rad tim iskustvo tražimo radi kod će su ste vas vaš naš naše mi ćete imate kroz više pored је да су није што ће сви су као који која које или од до се на за са у не га им ових овај ова ово наш ваш тим искуство"),
	"ru": wordSet("и в на для с по от что мы вы это работы команде опыт не как или из к за у о а но все наш наша ваш будет опыта работа"),
	"be": wordSet("і ў з па ад што гэта камандзе вопыт як або але ўсе будзе працаваць шукаем хто бо працы платформай намі адной каб калі таксама можа праз пры ўсіх усіх ёсць быў яны гэтай гэты іх чым ужо разам расці"),
	"kk": wordSet("және үшін бұл мен біз сіз бір бен да де не бойынша жұмыс командамызға тәжірибе қажет барлық адамдар олар бізбен бірге қалайды маңызды іздейміз"),
	"es": wordSet("el los las y que por para es son su sus más pero sobre nuestro nuestra nuestros equipo experiencia trabajo buscamos eres tu tus también ser estar muy años empresa puesto será serás nosotros quieren todos clientes desarrollo"),
	"fr": wordSet("le les du une et est sont pour avec dans sur par au aux nous vous votre notre vos nos qui que ce cette ses son équipe expérience poste rejoindre travail ou où pas plus ainsi sera être avoir recherchons personne entreprise"),
	"tr": wordSet("ve bir bu için ile çok daha gibi olan ya veya ise tüm ancak sen siz biz bizim deneyim ekip ekibimize iş arıyoruz sahip olarak üzerinde kadar sonra ayrıca görev pozisyon bizimle mühendis önemli"),
}

// decoyStops are languages the product has no template for but that share most function words with a template language
// (Portuguese and Galician with Spanish, Italian with Spanish and French). They only compete: when one of them wins, the text is
// und, never the look-alike template language (JSK-T167 review: a Portuguese posting must not be accepted as Spanish).
var decoyStops = map[string]map[string]bool{
	"pt": wordSet("não você uma são também nossa nosso nossos nossas está estão têm mais ou como seu sua seus suas ao aos às pelo pela pelos pelas muito já ainda então porque é os as do da dos das em um equipe pessoas responsável desenvolvimento times salário"),
	"gl": wordSet("unha os as noso nosa nosos nosas connosco traballe persoa desenvolvemento nós máis xa tamén sen onde ao aos coa coas cos cunha cun medrar candidatos posto"),
	"az": wordSet("və üçün ilə bizim sizə biz siz bir bu çox daha hər bizimlə axtarır vacibdir komanda çalışacaqsınız məlumat yaxşı imkanı veririk şirkətimiz təcrübə"),
	"uz": wordSet("va uchun bilan biz siz bir bu juda ham mijozlarimiz jamoamizga qidirmoqdamiz ishlaysiz tizimlar talablar tajriba yaxshi taklif qilamiz"),
	"it": wordSet("il lo gli della delle dei degli nel nella nello per che non sono uno noi nostro nostra nostri cerchiamo gruppo esperienza sviluppo clienti vogliamo sulla piattaforma"),
}

var srCyrMarks = "јљњћђџЈЉЊЋЂЏ"
var ruCyrMarks = "ыэъёЫЭЪЁ"
var kkCyrMarks = "әғқңөұүһӘҒҚҢӨҰҮҺ"

// persianLetters occur in Persian but not in Arabic spelling; arabicLetters (kaf and yeh) are the Arabic forms.
// psKuLetters occur in Pashto and Sorani Kurdish but not in Persian or Arabic: such text is und, never fa (JSK-T167 review B2).
const psKuLetters = "ټډړږښګځڅۍێۆڵڕەڤ"

const persianLetters = "پچژگکی"
const arabicLetters = "كي"

func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
}

// scriptLang decides the languages that a script identifies on its own (the other six template languages: ja, ko, zh, hi, ar, fa).
// It returns "" when the dominant script is Latin or Cyrillic.
func scriptLang(n map[string]int, tot int, persian, arabic, psKu int) string {
	switch {
	case n["hangul"]*3 > tot:
		return "ko"
	case n["kana"] >= 3 && (n["kana"]+n["han"])*2 > tot:
		return "ja"
	case n["han"]*2 > tot:
		return "zh"
	case n["deva"]*2 > tot:
		return "hi"
	case n["arab"]*2 > tot:
		if psKu >= 2 {
			return "?"
		}
		if persian > arabic {
			return "fa"
		}
		return "ar"
	}
	return ""
}

// DetectLanguage returns (code, confidence). Codes: the 15 template languages (ar be de en es fa fr hi ja kk ko ru sr tr zh) and und.
// Script-identified languages carry a fixed 0.9 once at least 20 letters of the script are present; Latin and Cyrillic text is
// decided by stop-word tables and the confidence formula below. Limits, stated: Marathi and Nepali (Devanagari) read as hi, Urdu
// (Arabic script with Persian letters) as fa, Ukrainian as ru or be, Italian, Polish, Portuguese, Galician, Azerbaijani, Uzbek, Pashto and Sorani Kurdish stay und via decoy tables and unique letters. The look-alike class is open-ended by nature (a stop-word detector cannot know every language): every new look-alike found is added as a decoy plus a test row, and the operator confirmation (confirm_de, confirm_fallback, explicit-language override) remains the backstop.
func DetectLanguage(text string) (string, float64) {
	n := map[string]int{}
	var cyr, lat int
	srM, ruM, kkM, beM, persian, arabic, psKu := 0, 0, 0, 0, 0, 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
			switch {
			case strings.ContainsRune(srCyrMarks, r):
				srM++
			case strings.ContainsRune(kkCyrMarks, r):
				kkM++
			case r == 'ў' || r == 'Ў':
				beM++
			}
			if strings.ContainsRune(ruCyrMarks, r) {
				ruM++
			}
		case unicode.Is(unicode.Latin, r):
			lat++
		case unicode.Is(unicode.Hangul, r):
			n["hangul"]++
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			n["kana"]++
		case unicode.Is(unicode.Han, r):
			n["han"]++
		case unicode.Is(unicode.Devanagari, r):
			n["deva"]++
		case unicode.Is(unicode.Arabic, r):
			n["arab"]++
			if strings.ContainsRune(persianLetters, r) {
				persian++
			}
			if strings.ContainsRune(arabicLetters, r) {
				arabic++
			}
			if strings.ContainsRune(psKuLetters, r) {
				psKu++
			}
		}
	}
	tot := cyr + lat + n["hangul"] + n["kana"] + n["han"] + n["deva"] + n["arab"]
	if tot < 40 { // too little text for any claim; CJK and Hangul carry more per letter, so the floor is lower there
		if tot < 20 || cyr+lat > 0 {
			return "und", 0
		}
	}
	if l := scriptLang(n, tot, persian, arabic, psKu); l == "?" {
		return "und", 0.3
	} else if l != "" {
		return l, 0.9
	}
	toks := tokens(text)
	hits := map[string]int{}
	for _, t := range toks {
		for l, set := range langStops {
			if set[t] {
				hits[l]++
			}
		}
		for l, set := range decoyStops {
			if set[t] {
				hits[l]++
			}
		}
	}
	if cyr*2 > tot {
		pick := func(first, second string) (string, float64) {
			return first, conf(hits[first], hits[second], len(toks), 0.9)
		}
		switch {
		case beM > 0: // the letter ў occurs only in Belarusian: decisive, so Russian-shared words do not dilute the share
			return "be", conf(hits["be"], 0, len(toks), 0.9)
		case kkM > 0 && kkM > srM:
			return "kk", conf(hits["kk"], 0, len(toks), 0.9)
		case srM > 0 && srM >= ruM:
			return "sr", conf(hits["sr"], 0, len(toks), 0.9)
		case hits["ru"] > 0 || ruM > 0 || hits["be"] > 0:
			if hits["be"] > hits["ru"] {
				return pick("be", "ru")
			}
			return "ru", conf(hits["ru"], hits["be"], len(toks), 0.9)
		}
		return "und", 0.3
	}
	// Latin script. Serbian Latin diacritics are a corroborating marker only.
	best, bestN, secN := "", 0, 0
	for _, l := range []string{"de", "en", "es", "fr", "tr", "sr", "pt", "gl", "it", "az", "uz"} { // fixed order: ties resolve deterministically; the last five are decoys
		h := hits[l]
		if h > bestN {
			secN = bestN
			best, bestN = l, h
		} else if h > secN {
			secN = h
		}
	}
	if bestN == 0 {
		return "und", 0.2
	}
	if _, decoy := decoyStops[best]; decoy {
		return "und", 0.3
	}
	return best, conf(bestN, secN, len(toks), 1.0)
}

// conf = (share of the winner among all stop-word hits) x (coverage saturating at ~10% stop-word density) x cap.
func conf(win, other, ntok int, capv float64) float64 {
	if win == 0 || ntok == 0 {
		return 0
	}
	share := float64(win) / float64(win+other)
	cov := math.Min(1, float64(win)/float64(ntok)/0.10)
	return math.Round(share*cov*capv*1000) / 1000
}

// AcceptLanguage applies the FR-021 rule: the detected code when confidence >= 0.8, else "und".
func AcceptLanguage(text string) (string, float64) {
	l, c := DetectLanguage(text)
	if c < LangMinConfidence {
		return "und", c
	}
	return l, c
}
