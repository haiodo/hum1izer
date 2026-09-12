package cli

import (
	"testing"

	"github.com/haiodo/hum1izer/internal/code"
)

// Перцентили и цена предела считаются по отсортированному списку. Ошибка на
// единицу здесь тихо сдвинет предложенный предел.
func TestCalibrateMath(t *testing.T) {
	sorted := []int{1, 1, 1, 1, 1, 2, 2, 3, 5, 40}
	if got := pct(sorted, 50); got != 2 {
		t.Errorf("медиана %d, ожидалось 2", got)
	}
	if got := pct(sorted, 90); got != 40 {
		t.Errorf("p90 %d, ожидалось 40", got)
	}
	if got := upTo(sorted, 1); got != 5 {
		t.Errorf("не больше 1: %d, ожидалось 5", got)
	}
	if got := len(sorted) - upTo(sorted, 2); got != 3 {
		t.Errorf("больше 2: %d, ожидалось 3", got)
	}
	if got := pct(nil, 90); got != 0 {
		t.Errorf("пустой список дал %d", got)
	}
}

// Шапка лицензии не участвует в калибровке: проверка её тоже не смотрит, иначе
// четырнадцать строк копирайта задрали бы предел всему репозиторию.
func TestCalibrateSkipsLicense(t *testing.T) {
	lic := code.Comment{Start: 1, Lines: 5, Text: "Copyright 2024 Acme Inc.\nLicensed under the Apache License, Version 2.0;\nyou may not use this file except in compliance with the License.\nSee the License for the specific language governing permissions and\nlimitations under the License."}
	if !code.Ignored(lic) {
		t.Error("шапка лицензии не распознана")
	}
	keep := code.Comment{Start: 10, Lines: 1, Text: "hum1izer:keep оставлено намеренно"}
	if !code.Ignored(keep) {
		t.Error("пометка keep не распознана")
	}
	plain := code.Comment{Start: 10, Lines: 2, Text: "первая строка\nвторая строка"}
	if code.Ignored(plain) {
		t.Error("обычный комментарий пропущен мимо калибровки")
	}
	if p, l := code.Shape(plain); p != 2 || l != len([]rune("первая строка")) {
		t.Errorf("Shape дал %d строк, самая длинная %d", p, l)
	}
}
