package humanize

import "testing"

// Тесты правил лежат в самих наборах (tests: [{text, expect}]) и здесь
// прогоняются по всем трём встроенным. Проверяется конкретное правило через
// rule.find, а не факт находки вообще: иначе тест проходил бы за счёт соседа,
// а сломанная регулярка молча переставала бы ловить своё.
func TestRulesWithTests(t *testing.T) {
	covered := 0
	for _, set := range []string{"ru", "en", "code"} {
		rs, err := LoadBuiltin(set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		check := func(r Rule) {
			if len(r.Tests) == 0 {
				return
			}
			covered++
			for _, tt := range r.Tests {
				if got := len(r.find(tt.Text)) > 0; got != tt.Expect {
					t.Errorf("[%s] %q: текст %q: сработало %v, ожидалось %v",
						set, r.Name, tt.Text, got, tt.Expect)
				}
			}
		}
		for _, r := range rs.HardBans {
			check(r)
		}
		for _, c := range rs.Categories {
			for _, r := range c.Rules {
				check(r)
			}
		}
	}
	if covered == 0 {
		t.Fatal("ни у одного правила нет tests: покрытие утекло")
	}
	t.Logf("правил с tests: %d", covered)
}
