package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

var names = map[string]string{"en": "English", "ru": "Русский", "sr": "Српски", "be": "Беларуская", "de": "Deutsch", "es": "Español", "fr": "Français",
	"zh": "中文", "ja": "日本語", "ko": "한국어", "ar": "العربية", "fa": "فارسی", "hi": "हिन्दी", "tr": "Türkçe", "kk": "Қазақша"}

// Name is the language's own name (endonym) for the switcher; it is a proper name, not a translated string.
func Name(lang string) string { return names[lang] }

// Negotiate picks the interface language from an Accept-Language header. It returns the first preferred language that is
// both required and shipped, and, when a more-preferred required language is NOT shipped yet, that language as
// unavailable so the page can say so — an unshipped language is never silently replaced (FR-069).
func Negotiate(header string, shipped []string) (lang, unavailable string) {
	have := map[string]bool{}
	for _, s := range shipped {
		have[s] = true
	}
	req := map[string]bool{}
	for _, r := range Required {
		req[r] = true
	}
	type pref struct {
		base string
		q    float64
		i    int
	}
	var ps []pref
	for i, part := range strings.Split(header, ",") {
		fs := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fs[0]))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, f := range fs[1:] {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "q=") {
				if v, err := strconv.ParseFloat(f[2:], 64); err == nil {
					q = v
				} else {
					q = 0
				}
			}
		}
		if q <= 0 {
			continue
		}
		ps = append(ps, pref{strings.SplitN(tag, "-", 2)[0], q, i})
	}
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].q > ps[b].q })
	for _, p := range ps {
		if !req[p.base] {
			continue
		}
		if have[p.base] {
			return p.base, unavailable
		}
		if unavailable == "" {
			unavailable = p.base
		}
	}
	return Source, unavailable
}

// date layouts per language; English stays ISO because that is what the console has always shown.
var dateLayout = map[string]string{"en": "2006-01-02", "de": "02.01.2006", "ru": "02.01.2006", "be": "02.01.2006", "sr": "02.01.2006", "kk": "02.01.2006", "tr": "02.01.2006",
	"es": "02/01/2006", "fr": "02/01/2006", "ar": "02/01/2006", "fa": "02/01/2006", "hi": "02/01/2006", "ja": "2006/01/02", "zh": "2006/01/02", "ko": "2006. 1. 2."}

// Date formats the calendar date of t for lang. Unknown languages use the source layout. Digits stay ASCII for dates (the
// audit log and exports are ISO regardless; this is display only).
func Date(lang string, t time.Time) string {
	l, ok := dateLayout[lang]
	if !ok {
		l = dateLayout[Source]
	}
	return t.Format(l)
}

func tag(lang string) language.Tag {
	if _, ok := names[lang]; !ok {
		return language.English
	}
	return language.Make(lang)
}

// Number formats f with the language's grouping and decimal separators (CLDR via x/text).
func Number(lang string, f float64) string {
	return message.NewPrinter(tag(lang)).Sprint(number.Decimal(f))
}

// suffixCurrency lists languages whose convention writes the currency sign after the amount (with a no-break space).
var suffixCurrency = map[string]bool{"de": true, "es": true, "fr": true, "ru": true, "be": true, "sr": true, "kk": true, "ar": true, "fa": true}

// Money formats amount in the ISO 4217 code with the language's separators and sign placement; always two fraction digits.
func Money(lang string, amount float64, code string) string {
	u, err := currency.ParseISO(code)
	if err != nil {
		return fmt.Sprintf("%.2f %s", amount, code)
	}
	p := message.NewPrinter(tag(lang))
	n := p.Sprint(number.Decimal(amount, number.MinFractionDigits(2), number.MaxFractionDigits(2)))
	sign := message.NewPrinter(language.English).Sprint(currency.NarrowSymbol(u))
	if suffixCurrency[lang] {
		return n + "\u00a0" + sign
	}
	return sign + n
}
