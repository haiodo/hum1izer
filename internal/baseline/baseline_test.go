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
	if s.Size() != 0 || s.Known(Entry{"a", "b"}) {
		t.Error("пустой снимок что-то знает")
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.txt")
	want := []Entry{
		{"aaaaaaaaaaaa", "Баннер-разделитель"},
		{"bbbbbbbbbbbb", "TODO без владельца"},
		{"bbbbbbbbbbbb", "TODO без владельца"}, // дубль схлопывается
	}
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	n := 0
	for _, l := range strings.Split(string(body), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("строк с находками: %d, ожидалось 2\n%s", n, body)
	}
	// Сортировка по хэшу: aaa... должен идти раньше bbb...
	if strings.Index(string(body), "\naaaaaaaaaaaa") > strings.Index(string(body), "\nbbbbbbbbbbbb") {
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
	if s.Known(Entry{"cccccccccccc", "Новое"}) {
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

func TestReadsV1Snapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.txt")
	body := "# hum1izer baseline v1\naaaaaaaaaaaa\tБаннер-разделитель\tsrc/a.go\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Known(Entry{"aaaaaaaaaaaa", "Баннер-разделитель"}) {
		t.Error("запись из старого снимка не узнана")
	}
}

func TestOneLinePerComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base.txt")
	err := Write(path, []Entry{
		{"aaaaaaaaaaaa", "Второе"},
		{"aaaaaaaaaaaa", "Первое"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	want := "aaaaaaaaaaaa\t" + Code("Второе") + "\t" + Code("Первое")
	alt := "aaaaaaaaaaaa\t" + Code("Первое") + "\t" + Code("Второе")
	if !strings.Contains(string(body), want) && !strings.Contains(string(body), alt) {
		t.Errorf("два правила одного блока не слились в строку: %q", string(body))
	}
	if !strings.Contains(string(body), "# "+Code("Первое")+" Первое") {
		t.Error("в шапке нет расшифровки кода правила")
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Size() != 2 {
		t.Errorf("после чтения %d записей, ожидалось 2", s.Size())
	}
}

func TestReadsV2Snapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.txt")
	body := "# hum1izer baseline v2\nbbbbbbbbbbbb\tTODO без владельца\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Known(Entry{"bbbbbbbbbbbb", "TODO без владельца"}) {
		t.Error("запись снимка v2 не узнана")
	}
}
