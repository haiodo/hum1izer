package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashIgnoresWhitespace(t *testing.T) {
	if Hash("раз   два\n  три") != Hash("раз два три") {
		t.Error("переотступ поменял хэш")
	}
	if Hash("раз два") == Hash("раз три") {
		t.Error("разный текст дал одинаковый хэш")
	}
}

func TestMissingFileIsEmptySet(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "нет.txt"))
	if err != nil {
		t.Fatalf("первый прогон стал ошибкой: %v", err)
	}
	if s.Size() != 0 || s.Known(Entry{"a", "b", "c"}) {
		t.Error("пустой снимок что-то знает")
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.txt")
	want := []Entry{
		{"aaaaaaaaaaaa", "Баннер-разделитель", "b.go"},
		{"bbbbbbbbbbbb", "TODO без владельца", "a.go"},
		{"bbbbbbbbbbbb", "TODO без владельца", "a.go"}, // дубль схлопывается
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if n := strings.Count(string(body), "\n") - strings.Count(header, "\n"); n != 2 {
		t.Errorf("строк с находками: %d, ожидалось 2\n%s", n, body)
	}
	// Сортировка по файлу: a.go должен идти раньше b.go.
	if strings.Index(string(body), "a.go") > strings.Index(string(body), "b.go") {
		t.Error("снимок не отсортирован, диффы будут шумными")
	}

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Size() != 2 {
		t.Fatalf("в снимке %d записей", s.Size())
	}
	if !s.Known(want[0]) {
		t.Error("известная находка не узнана")
	}
	if s.Known(Entry{"cccccccccccc", "Новое", "a.go"}) {
		t.Error("незнакомая находка узнана")
	}
	if s.Fixed() != 1 {
		t.Errorf("исправлено: %d, ожидалось 1 (вторую не видели)", s.Fixed())
	}
}

func TestBrokenFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.txt")
	if err := os.WriteFile(path, []byte("мусор без табуляций\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("битый снимок принят молча")
	}
}
