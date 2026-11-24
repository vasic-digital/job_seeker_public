package match

import "testing"

// Logic tests only. Real-posting measurement: evidence/JSK-T053 (EN measured; RU/SR/DE not measurable on postings).
func TestDetectLanguageBranches(t *testing.T) {
	cases := map[string]string{
		"We are looking for an engineer to join our team and work on the platform that you will love with strong skills and experience in building systems":            "en",
		"Wir suchen eine Entwicklerin für unser Team, die mit uns an der Plattform arbeitet und die Erfahrung in der Entwicklung von Systemen hat, sie ist willkommen": "de",
		"Мы ищем разработчика в нашу команду для работы над платформой, опыт работы не менее трёх лет, вы будете работать с нами и это важно для всех":                 "ru",
		"Тражимо програмера за наш тим који ће радити на платформи, искуство је важно, и ћете радити са нама као део тима за све кориснике":                            "sr",
	}
	for text, want := range cases {
		if got, c := AcceptLanguage(text); got != want || c < LangMinConfidence {
			t.Errorf("want %s got %s %.2f for %.30q", want, got, c, text)
		}
	}
	if l, _ := AcceptLanguage("short"); l != "und" {
		t.Error("short text must be und")
	}
	a, ac := DetectLanguage(cases["We are looking for an engineer to join our team and work on the platform that you will love with strong skills and experience in building systems"])
	b, bc := DetectLanguage(cases["We are looking for an engineer to join our team and work on the platform that you will love with strong skills and experience in building systems"])
	if a != b || ac != bc {
		t.Error("nondeterministic")
	}
}
