package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Модель возвращает текст как умеет: в блоке ```, с маркерами комментария, в
// кавычках. Всё это надо снять, иначе маркеры удвоятся при вставке.
func TestCleanStripsWhatModelAdds(t *testing.T) {
	cases := []struct{ in, want string }{
		{"обычный текст", "обычный текст"},
		{"```\n// текст\n```", "текст"},
		{"```go\n// текст\n```", "текст"},
		{"// первая\n// вторая", "первая\nвторая"},
		{"/**\n * первая\n * вторая\n */", "первая\nвторая"},
		{"# текст", "текст"},
		{`"в кавычках"`, "в кавычках"},
		{`он сказал "нет" и ушёл`, `он сказал "нет" и ушёл`},
		{"  \n текст \n  \n", "текст"},
	}
	for _, c := range cases {
		if got := clean(c.in); got != c.want {
			t.Errorf("clean(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestNewRequiresModelAndKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := New(Config{}); err == nil {
		t.Error("без модели клиент собрался")
	}
	if _, err := New(Config{Model: "m"}); err == nil {
		t.Error("без ключа и без base_url клиент собрался")
	}
	// Локальному серверу ключ не нужен: base_url задан - этого хватает.
	if _, err := New(Config{Model: "m", BaseURL: "http://localhost:11434/v1/"}); err != nil {
		t.Errorf("локальный адрес без ключа: %v", err)
	}
}

// Полный круг против заглушки: запрос уходит на /chat/completions, в нём есть
// комментарий, код и претензии правил, ответ разбирается и чистится.
func TestRewriteAgainstStub(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "object": "chat.completion", "model": "stub",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "// короче некуда"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 7, "total_tokens": 127},
		})
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/v1/", Model: "stub"})
	if err != nil {
		t.Fatal(err)
	}
	got, use, err := c.Rewrite(context.Background(), Request{
		Comment:  "// длинный комментарий про всё на свете",
		Code:     "func f() {}",
		Findings: []string{"Длинный комментарий: уложись в 2 строки"},
		MaxLines: 2,
		Lang:     "ru",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "короче некуда" {
		t.Errorf("ответ %q, ожидался %q", got, "короче некуда")
	}
	if use.In != 120 || use.Out != 7 {
		t.Errorf("токены %+v, ожидалось 120/7", use)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Errorf("запрос ушёл на %q", gotPath)
	}
	for _, want := range []string{"длинный комментарий про всё", "func f() {}", "уложись в 2 строки", "русский"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("в запросе нет %q", want)
		}
	}
}

// Модель не знает ширину строки, по которой её ответ потом нарежут, поэтому
// бюджет в промпте назван в символах. Без него она пишет длиннее исходника.
func TestPromptCarriesCharBudget(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "1", "object": "chat.completion", "model": "stub",
			"choices": []map[string]any{{"index": 0,
				"message": map[string]any{"role": "assistant", "content": "коротко"}}},
		})
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/v1/", Model: "stub"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Rewrite(context.Background(), Request{
		Comment: "// длинный комментарий", MaxLines: 2, MaxLine: 100, Lang: "ru",
	}); err != nil {
		t.Fatal(err)
	}
	// 2 строки по 90 знаков: и бюджет, и обе величины должны быть в промпте.
	for _, want := range []string{"180 символов", "2 строк", "по 90"} {
		if !strings.Contains(body, want) {
			t.Errorf("в промпте нет %q", want)
		}
	}

	// Без настроек берутся значения по умолчанию, а не ноль.
	if _, _, err := c.Rewrite(context.Background(), Request{Comment: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "180 символов") {
		t.Errorf("без настроек бюджет не по умолчанию: %s", body[:min(len(body), 400)])
	}
}

// Отказ по лимиту узнаётся по коду или по словам, срок берётся из тела.
func TestRetryAfterReadsLimit(t *testing.T) {
	cases := []struct {
		msg  string
		want time.Duration
		ok   bool
	}{
		{`POST "/v1/chat/completions": 429 Too Many Requests {"message":"qwen3.8-27b rate limit 12/min reached — retry in 7s."}`, 8 * time.Second, true},
		{"429 Too Many Requests", 15 * time.Second, true},
		{"rate limit exceeded, retry after 30 s", 31 * time.Second, true},
		{"500 Internal Server Error", 0, false},
		{"context deadline exceeded", 0, false},
	}
	for _, c := range cases {
		got, ok := RetryAfter(errors.New(c.msg))
		if ok != c.ok || got != c.want {
			t.Errorf("RetryAfter(%q) = %v, %v; ожидалось %v, %v", c.msg, got, ok, c.want, c.ok)
		}
	}
	if _, ok := RetryAfter(nil); ok {
		t.Error("nil принят за отказ по лимиту")
	}
}
