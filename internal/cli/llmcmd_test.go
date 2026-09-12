package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/llm"
)

func TestMaskKeyHidesSecret(t *testing.T) {
	cases := map[string]string{
		"":                         "не задан",
		"short":                    "задан",
		"sk-proj-abcdefgh1234WXYZ": "...WXYZ",
	}
	for in, want := range cases {
		if got := maskKey(in); got != want {
			t.Errorf("maskKey(%q) = %q, ожидалось %q", in, got, want)
		}
		if in != "" && strings.Contains(maskKey(in), in) {
			t.Errorf("maskKey(%q) вернул ключ целиком", in)
		}
	}
}

// Выбор записывается только по enter: q и esc уходят ни с чем, иначе случайный
// выход менял бы модель.
func TestModelPickerNeedsEnter(t *testing.T) {
	m := modelPicker{models: []string{"a", "b", "c"}, current: "a"}
	send := func(mod tea.Model, k tea.KeyMsg) tea.Model {
		next, _ := mod.Update(k)
		return next
	}
	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	after := send(send(m, down), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}).(modelPicker)
	if after.chosen != "" {
		t.Errorf("по q выбрано %q", after.chosen)
	}
	after = send(send(m, down), tea.KeyMsg{Type: tea.KeyEnter}).(modelPicker)
	if after.chosen != "b" {
		t.Errorf("по enter выбрано %q, ожидалось b", after.chosen)
	}
	if !strings.Contains(m.View(), "(current)") {
		t.Error("текущая модель не помечена")
	}
}

func newLLMForTest(t *testing.T, model string) (*llm.Client, string) {
	t.Helper()
	c, err := llm.New(llm.Config{BaseURL: "http://localhost:1/v1/", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	return c, ""
}
