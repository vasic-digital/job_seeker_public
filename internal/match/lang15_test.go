package match

import "testing"

// JSK-T167 (F-01, G-05): one synthetic posting per template language; each must be detected with confidence >= LangMinConfidence.
// Texts are invented, generic and carry no real company or person.
var lang15 = map[string]string{
	"en": "We are looking for an engineer to join our team and work on the platform that you will love, with strong skills and experience in building the systems of our customers and all of their services",
	"de": "Wir suchen eine Entwicklerin für unser Team, die mit uns an der Plattform arbeitet und die Erfahrung in der Entwicklung von Systemen hat, sie ist bei uns willkommen und wird von allen unterstützt",
	"ru": "Мы ищем разработчика в нашу команду для работы над платформой, опыт работы не менее трёх лет, вы будете работать с нами и это важно для всех, кто хочет расти вместе с нами",
	"sr": "Тражимо програмера за наш тим који ће радити на платформи, искуство је важно, и ћете радити са нама као део тима за све кориснике који су нам важни",
	"es": "Buscamos una persona para nuestro equipo que trabaje con nosotros en la plataforma, con experiencia en el desarrollo de sistemas y que sea parte de una empresa para todos los que quieren crecer con los clientes",
	"fr": "Nous recherchons une personne pour rejoindre notre équipe et travailler avec nous sur la plateforme, avec de l'expérience dans le développement des systèmes pour les clients, et qui est prête à rejoindre une entreprise dans laquelle vous serez au centre",
	"tr": "Ekibimize katılacak ve bizimle birlikte platform üzerinde çalışacak bir mühendis arıyoruz, bu rol için sistemlerin geliştirilmesinde deneyim ve çok iyi bir ekip ruhu gibi özellikler de önemli, ancak en önemlisi bir öğrenme isteği olmasıdır",
	"be": "Мы шукаем распрацоўшчыка ў нашу каманду для працы над платформай, вопыт працы не менш за тры гады, і гэта важна для ўсіх, хто хоча расці разам з намі, бо мы ўсе працуем у адной камандзе",
	"kk": "Біз өз командамызға платформа жүйесінде жұмыс істейтін әзірлеушіні іздейміз, бұл жұмыс үшін тәжірибе қажет және біз үшін барлық адамдар маңызды, олар бізбен бірге өсуді қалайды",
	"ar": "نبحث عن مهندس برمجيات للانضمام إلى فريقنا والعمل معنا على المنصة، ولديه خبرة في تطوير الأنظمة والخدمات للعملاء في جميع أنحاء العالم",
	"fa": "ما به دنبال یک مهندس نرم‌افزار برای پیوستن به تیم خود هستیم تا با ما روی پلتفرم کار کند و در توسعه سیستم‌ها و خدمات برای مشتریان تجربه داشته باشد",
	"hi": "हम अपनी टीम में शामिल होने के लिए एक इंजीनियर की तलाश कर रहे हैं जो हमारे साथ प्लेटफ़ॉर्म पर काम करे और ग्राहकों के लिए सिस्टम बनाने का अनुभव रखता हो",
	"ja": "私たちのチームに参加して、プラットフォームの開発に取り組んでくれるエンジニアを募集しています。お客様のためのシステム開発の経験がある方を歓迎します。",
	"ko": "저희 팀에 합류하여 플랫폼 개발을 함께 진행할 엔지니어를 찾고 있습니다. 고객을 위한 시스템 개발 경험이 있는 분을 환영합니다.",
	"zh": "我们正在寻找一名工程师加入我们的团队，与我们一起开发平台。我们欢迎有为客户开发系统经验的人才加入。",
}

func TestDetectsAllFifteenTemplateLanguages(t *testing.T) {
	if len(lang15) != 15 {
		t.Fatalf("table has %d languages", len(lang15))
	}
	for want, text := range lang15 {
		got, c := AcceptLanguage(text)
		if got != want || c < LangMinConfidence {
			t.Errorf("%s: got %s conf %.2f", want, got, c)
		}
	}
}

func TestSerbianLatinIsSerbian(t *testing.T) {
	txt := "Tražimo programera za naš tim koji će raditi na platformi, iskustvo je važno, i radićete sa nama kao deo tima za sve korisnike koji su nam važni i koji se vezuju za nas"
	if l, c := AcceptLanguage(txt); l != "sr" || c < LangMinConfidence {
		t.Fatalf("%s %.2f", l, c)
	}
}

func TestUnsupportedLanguageIsNotMistakenForATemplateLanguage(t *testing.T) {
	// Italian and Polish are not template languages; they must not be reported with high confidence as one of the 15.
	it := "Cerchiamo una persona per il nostro gruppo che lavori con noi sulla piattaforma, con esperienza nello sviluppo di sistemi per i clienti e che voglia crescere insieme a tutti noi"
	pl := "Szukamy osoby do naszego zespołu, która będzie pracować z nami nad platformą, z doświadczeniem w tworzeniu systemów dla klientów i która chce się rozwijać razem z nami"
	pt := "Somos uma empresa de tecnologia que procura um engenheiro de software para a nossa equipe. Você será responsável pelo desenvolvimento de sistemas para os clientes e por trabalhar com todos os times. Buscamos pessoas que queiram crescer e que tenham experiência em Go. Também é importante ser comunicativo e estar disponível para viagens. Os benefícios são muitos e o salário é competitivo para o mercado."
	gl := "Buscamos unha persoa para o noso equipo que traballe connosco na plataforma, con experiencia no desenvolvemento de sistemas para os clientes e que queira medrar con todos nós na empresa. Os candidatos deben ser proactivos e ter máis de tres anos de experiencia para o posto."
	az := "Şirkətimiz bir proqramçı axtarır. Bu iş üçün təcrübə vacibdir və siz bizim komanda ilə birlikdə çalışacaqsınız. Hər bir əməkdaş bizim üçün çox önəmlidir, daha çox məlumat üçün bizimlə əlaqə saxlayın. Biz sizə yaxşı maaş və inkişaf imkanı veririk."
	uz := "Biz jamoamizga qo'shilish uchun tajribali dasturchi qidirmoqdamiz. Siz platforma ustida ishlaysiz va mijozlarimiz uchun tizimlar qurasiz. Talablar: Go va Kubernetes bilan tajriba, ingliz tilini yaxshi bilish. Biz raqobatbardosh maosh va uydan ishlash imkoniyatini taklif qilamiz."
	ps := "موږ زموږ ټیم سره د یوځای کیدو لپاره د تجربه لرونکي سافټویر انجینر په لټه کې یو چې زموږ سره په پلیټ فارم کار وکړي او د پیرودونکو لپاره سیسټمونه جوړ کړي"
	ckb := "ئێمە بەدوای ئەندازیارێکی نەرمەکاڵای بەئەزموون دەگەڕێین بۆ ئەوەی بێتە ناو تیمەکەمانەوە و لەگەڵ ئێمە لەسەر پلاتفۆرمەکە کار بکات"
	for _, txt := range []string{it, pl, pt, gl, az, uz, ps, ckb} {
		if l, c := AcceptLanguage(txt); l != "und" {
			t.Errorf("unsupported language accepted as %s (%.2f)", l, c)
		}
	}
}
