// Package llm просит модель переписать комментарий. Любой OpenAI-совместимый
// адрес: локальный llama.cpp, vllm, openrouter, сам OpenAI.
package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Config struct {
	BaseURL string // пусто - api.openai.com
	Key     string // пусто - берётся из OPENAI_API_KEY
	Model   string // пусто - только для списка моделей
}

type Client struct {
	api   openai.Client
	model string
}

// Локальные серверы ключа не спрашивают, но openai-go без него ругается на
// каждый запрос, поэтому подставляется заглушка.
const localKey = "no-key"

// New собирает клиента. Модель здесь не обязательна: за списком моделей ходят
// как раз тогда, когда её ещё не выбрали.
func New(c Config) (*Client, error) {
	key := c.Key
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	opts := []option.RequestOption{}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
		if key == "" {
			key = localKey
		}
	}
	if key == "" {
		return nil, errors.New("ключ не задан: OPENAI_API_KEY, --llm-key или llm.key в .hum1izer.yaml")
	}
	opts = append(opts, option.WithAPIKey(key))
	return &Client{api: openai.NewClient(opts...), model: c.Model}, nil
}

// Model - на какой модели собран клиент, пусто если не выбрана.
func (c *Client) Model() string { return c.model }

// Models - список моделей эндпоинта: ими заполняется выбор в TUI.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	page, err := c.api.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range page.Data {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("эндпоинт не вернул ни одной модели")
	}
	return out, nil
}

// Request - всё, что модель должна видеть: сам комментарий, код вокруг него и
// претензии правил. Без кода модель пересказывает комментарий своими словами.
type Request struct {
	Comment  string
	Code     string
	Findings []string
	MaxLines int
	MaxLine  int    // предел длины строки из настроек
	Lang     string // ru или en - на каком языке написан комментарий
	Note     string // претензия к прошлому ответу, если просим переделать
}

// В промпте бюджет назван в символах, а не в строках: строки нарезает уже
// вставка, по ширине из настроек, и модель этой ширины не знает.
const system = `Ты правишь комментарии в коде. Верни только новый текст комментария.

Правила:
- Без маркеров //, /*, #, * - только текст.
- Весь ответ не длиннее %d символов: это %d строк по %d. Короче - лучше.
- Не разбивай на строки сам, перенос сделают за тебя. Абзац нужен - ставь перевод строки.
- Сохрани язык оригинала (%s) и все факты: имена, числа, условия, предупреждения.
- Убери пересказ кода, вводные обороты, восторги и воду. Оставь причину решения.
- Если сокращать нечего и комментарий уже по делу - верни его как есть.
- Не обрывай фразу на середине: лучше выбрось мысль целиком, чем оставь хвост.
- Никаких пояснений, кавычек и разметки вокруг ответа.`

// Usage - сколько токенов ушло и вернулось за один вызов. Локальные серверы
// поле иногда не заполняют, тогда останутся нули.
type Usage struct {
	In, Out int64
}

func (c *Client) Rewrite(ctx context.Context, r Request) (string, Usage, error) {
	if c.model == "" {
		return "", Usage{}, errors.New("модель не выбрана: hum1izer llm")
	}
	lang := "русский"
	if r.Lang != "ru" {
		lang = "английский"
	}
	maxLines := r.MaxLines
	if maxLines < 1 {
		maxLines = 2
	}
	maxLine := r.MaxLine
	if maxLine < 20 {
		maxLine = 100
	}
	// Запас на маркер и отступ: до них доберётся renderComment, а модели важен
	// порядок величины, а не точность до символа.
	budget := maxLines * (maxLine - 10)

	var b strings.Builder
	fmt.Fprintf(&b, "Комментарий:\n%s\n", r.Comment)
	if r.Code != "" {
		fmt.Fprintf(&b, "\nКод вокруг:\n%s\n", r.Code)
	}
	if len(r.Findings) > 0 {
		fmt.Fprintf(&b, "\nЧто с ним не так:\n- %s\n", strings.Join(r.Findings, "\n- "))
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "\n%s\n", r.Note)
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	resp, err := c.api.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(fmt.Sprintf(system, budget, maxLines, maxLine-10, lang)),
			openai.UserMessage(b.String()),
		},
	})
	if err != nil {
		return "", Usage{}, err
	}
	u := Usage{In: resp.Usage.PromptTokens, Out: resp.Usage.CompletionTokens}
	if len(resp.Choices) == 0 {
		return "", u, errors.New("модель вернула пустой ответ")
	}
	return clean(resp.Choices[0].Message.Content), u, nil
}

// retryRe вытаскивает срок из тела 429: у разных эндпоинтов он пишется
// по-разному, но секунды в тексте есть почти всегда.
var retryRe = regexp.MustCompile(`retry (?:in|after) (\d+)\s*s`)

// RetryAfter - сколько ждать после отказа по лимиту. Тип ошибки SDK лежит в
// internal и наружу не выведен, поэтому разбирается текст.
func RetryAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	if !strings.Contains(msg, "429") && !strings.Contains(strings.ToLower(msg), "rate limit") {
		return 0, false
	}
	if m := retryRe.FindStringSubmatch(msg); m != nil {
		if n, e := strconv.Atoi(m[1]); e == nil && n > 0 {
			return time.Duration(n)*time.Second + time.Second, true
		}
	}
	return 15 * time.Second, true
}

// clean снимает то, что модель добавляет поверх просьбы: блок ```, маркеры
// комментария в начале строк, кавычки вокруг всего ответа.
func clean(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		l = strings.TrimSpace(l)
		for _, p := range []string{"/**", "*/", "///", "//", "/*", "#", "*"} {
			if strings.HasPrefix(l, p) {
				l = strings.TrimSpace(l[len(p):])
				break
			}
		}
		if l != "" {
			out = append(out, l)
		}
	}
	s = strings.Join(out, "\n")
	if len(s) > 1 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && !strings.Contains(s[1:len(s)-1], `"`) {
		s = s[1 : len(s)-1]
	}
	return s
}
