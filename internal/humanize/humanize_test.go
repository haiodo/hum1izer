package humanize

import (
	"os"
	"testing"
)

func rules(t *testing.T) *RuleSet {
	t.Helper()
	rs, err := LoadRules("")
	if err != nil {
		t.Fatalf("встроенные правила не грузятся: %v", err)
	}
	return rs
}

func hitCount(hits []Hit, marker string) int {
	for _, h := range hits {
		if h.Marker == marker {
			return h.Count
		}
	}
	return 0
}

func TestHardBans(t *testing.T) {
	rs := rules(t)
	cases := []struct {
		text   string
		marker string
		want   int
	}{
		{"В современном мире всё меняется.", "В современном мире", 1},
		{"Данный подход применяется давно.", "Данный/Данная/Данное", 1},
		{"Данные показывают рост.", "Данный/Данная/Данное", 0}, // мн.ч. = существительное
		{"Это не просто утилита, а целый комбайн.", "Не просто X, а Y", 1},
		{"Стоит отметить, что тесты зелёные.", "Стоит отметить, что", 1},
		{"Комплексный подход решает всё.", "Комплексный подход/решение", 1},
		{"Отчёт за 2020–2024 годы.", "En dash", 0}, // числовой диапазон законен
		{"Отчёт – это важно.", "En dash", 1},
		{"Поездка от Москвы до Твери.", "от X до Y (ложный диапазон)", 0}, // имена собственные
		{"Работал от зари до зари.", "от X до Y (ложный диапазон)", 0},    // повтор слова
		{"Продаём от гвоздей до кораблей.", "от X до Y (ложный диапазон)", 1},
		{"Таким образом, мы закончили.", "Подводя итог / Таким образом", 1},
		{"Он устроен таким образом, что не ломается.", "Подводя итог / Таким образом", 0},
	}
	for _, c := range cases {
		got := hitCount(ScanHardBans(rs, c.text), c.marker)
		if got != c.want {
			t.Errorf("%q: %s = %d, ожидалось %d", c.text, c.marker, got, c.want)
		}
	}
}

func TestFreqBanIsYavlyaetsya(t *testing.T) {
	rs := rules(t)
	// «Является» банится только выше плотности 1 на 500 слов и от двух вхождений.
	one := ScanHardBans(rs, "Это является примером.")
	if hitCount(EffectiveHardBans(rs, one, 20), "Является") != 0 {
		t.Error("одно вхождение «является» не должно быть баном")
	}
	two := ScanHardBans(rs, "Это является примером. То является ошибкой.")
	if hitCount(EffectiveHardBans(rs, two, 20), "Является") != 2 {
		t.Error("два вхождения на 20 слов должны стать баном")
	}
}

func TestLatinHomoglyph(t *testing.T) {
	if got := findLatinInCyrillic("прoверка"); len(got) != 1 { // латинская o
		t.Errorf("гомоглиф не найден: %v", got)
	}
	if got := findLatinInCyrillic("проверка www.example.com"); len(got) != 0 {
		t.Errorf("законная латиница принята за гомоглиф: %v", got)
	}
}

func TestRhythmAndScore(t *testing.T) {
	rs := rules(t)
	flat := "Мы сделали отчёт вовремя. Они прислали ответ вчера. Он проверил цифры дважды. " +
		"Она закрыла задачу утром. Все успели к сроку."
	r := rhythm(flat)
	if r.Sentences != 5 {
		t.Fatalf("предложений: %d, ожидалось 5", r.Sentences)
	}
	if r.CVLen >= rs.Thresholds.CVHumanTarget {
		t.Errorf("ровный ритм не пойман: CV=%v", r.CVLen)
	}

	rep := Analyze(rs, "В современном мире данный подход является ключевым. Стоит отметить, что это важно.", "marketing")
	if rep.Score.Value >= rs.Thresholds.BandEdit {
		t.Errorf("текст из сплошных банов получил %d, ожидалось < %d", rep.Score.Value, rs.Thresholds.BandEdit)
	}
	if len(rep.HardBans) == 0 {
		t.Error("хард-баны не найдены")
	}
}

func TestGenreMuting(t *testing.T) {
	rs := rules(t)
	text := "Данный метод является основным. В связи с этим выводы таковы."
	strict := Analyze(rs, text, "marketing")
	academic := Analyze(rs, text, "academic")
	if len(academic.HardBans) >= len(strict.HardBans) {
		t.Errorf("жанр academic не снял банов: было %d, стало %d",
			len(strict.HardBans), len(academic.HardBans))
	}
	if academic.Score.Value <= strict.Score.Value {
		t.Errorf("балл academic (%d) должен быть выше строгого (%d)",
			academic.Score.Value, strict.Score.Value)
	}
}

func TestStructure(t *testing.T) {
	listicle := "# Заголовок\n\n- раз\n- два\n- три\n- четыре\n- пять\n- шесть\n"
	st := structure(listicle)
	if st.ListItems != 6 {
		t.Errorf("пунктов списка: %d, ожидалось 6", st.ListItems)
	}
	if !structure("Текст обрывается на полуслове и не завершён").TruncatedEnding {
		t.Error("обрыв на полуслове не пойман")
	}
	if structure("Текст завершён точкой.").TruncatedEnding {
		t.Error("завершённый текст принят за обрыв")
	}
}

func TestRulesValidate(t *testing.T) {
	// Опечатка в имени бана внутри жанра должна ломать загрузку, а не молчать.
	bad := []byte("hard_bans:\n  - name: Тест\n    lit: тест\ngenres:\n  marketing:\n    mute_bans: [Опечатка]\n")
	path := t.TempDir() + "/bad.yaml"
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRules(path); err == nil {
		t.Error("неизвестный бан в mute_bans принят молча")
	}
}

func TestCustomRulesFile(t *testing.T) {
	own := []byte("thresholds:\n  band_clean: 85\n  band_edit: 60\nhard_bans:\n" +
		"  - name: Своё правило\n    fix: убрать\n    word_re: 'синерги[\\p{L}]+'\n" +
		"genres:\n  marketing:\n    mute_bans: []\n")
	path := t.TempDir() + "/own.yaml"
	if err := os.WriteFile(path, own, 0o600); err != nil {
		t.Fatal(err)
	}
	rs, err := LoadRules(path)
	if err != nil {
		t.Fatalf("свой файл правил не загрузился: %v", err)
	}
	rep := Analyze(rs, "Мы ищем синергию во всём.", "marketing")
	if len(rep.HardBans) != 1 || rep.HardBans[0].Marker != "Своё правило" {
		t.Errorf("своё правило не сработало: %+v", rep.HardBans)
	}
}
