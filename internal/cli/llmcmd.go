package cli

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/llm"
)

const llmUsage = `hum1izer llm - endpoint, key and model for rewriting comments.

  hum1izer llm                     show current settings
  hum1izer llm --key <key>         save the key and pick a model from the list
  hum1izer llm --url <url>         same, for your own OpenAI-compatible server
  hum1izer llm --list              just show the list of models
  hum1izer llm --model <name>      set the model without picking from a list

Settings live in the user file, not in the repo: a key has no place there.
Path is $XDG_CONFIG_HOME/hum1izer/config.yaml, otherwise ~/.config/hum1izer/config.yaml,
mode 0600. The project .hum1izer.yaml and flags override them.
`

func runLLMCmd(args []string) int {
	fs := flag.NewFlagSet("llm", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, llmUsage) }
	url := fs.String("url", "", "base URL of an OpenAI-compatible API")
	key := fs.String("key", "", "API key")
	model := fs.String("model", "", "model name, without picking from a list")
	list := fs.Bool("list", false, "show the list of models and exit")
	clear := fs.Bool("forget", false, "erase saved endpoint, key and model")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	u, err := config.LoadUser()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *clear {
		u.LLM = config.LLM{}
		path, err := config.SaveUser(u)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("erased:", path)
		return 0
	}

	changed := false
	if *url != "" {
		u.LLM.BaseURL, changed = *url, true
	}
	if *key != "" {
		u.LLM.Key, changed = *key, true
	}
	if *model != "" {
		u.LLM.Model, changed = *model, true
	}

	// Ничего не просили - показываем состояние и выходим.
	if !changed && !*list {
		path, _ := config.UserPath()
		fmt.Println("file:  ", path)
		fmt.Println("url:   ", cmp.Or(u.LLM.BaseURL, "api.openai.com (default)"))
		fmt.Println("key:   ", maskKey(u.LLM.Key))
		fmt.Println("model: ", cmp.Or(u.LLM.Model, "not selected"))
		if u.LLM.Model == "" {
			fmt.Println("\npick one: hum1izer llm --key <key>")
		}
		return 0
	}

	// Адрес и ключ записываются сразу: список моделей может и не ответить, а
	// вводить их заново из-за этого незачем.
	if changed {
		path, err := config.SaveUser(u)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println("saved:", path)
	}
	if *model != "" {
		fmt.Println("model:", u.LLM.Model)
		return 0
	}

	client, err := llm.New(llm.Config{BaseURL: u.LLM.BaseURL, Key: u.LLM.Key})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	models, err := client.Models(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "model list:", err)
		return 2
	}
	if *list {
		for _, m := range models {
			fmt.Println(m)
		}
		return 0
	}

	picked, err := pickModel(models, u.LLM.Model)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if picked == "" {
		fmt.Println("nothing selected, nothing written")
		return 0
	}
	u.LLM.Model = picked
	path, err := config.SaveUser(u)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("model %s written to %s\n", picked, path)
	return 0
}

// maskKey: ключ показывается хвостом, чтобы отличить один от другого и не
// выложить его в лог или в скриншот целиком.
func maskKey(k string) string {
	switch {
	case k == "":
		return "not set"
	case len(k) <= 8:
		return "set"
	default:
		return "..." + k[len(k)-4:]
	}
}

// pickModel - список со стрелками. Отдельный экран, а не панель TUI: выбирают
// модель один раз, а живёт выбор в файле пользователя.
func pickModel(models []string, current string) (string, error) {
	m := modelPicker{models: models, current: current}
	for i, name := range models {
		if name == current {
			m.idx = i
		}
	}
	out, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	return out.(modelPicker).chosen, nil
}

type modelPicker struct {
	models  []string
	idx     int
	current string
	chosen  string
}

func (m modelPicker) Init() tea.Cmd { return nil }

func (m modelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.idx = clampIdx(m.idx-1, len(m.models))
	case "down", "j":
		m.idx = clampIdx(m.idx+1, len(m.models))
	case "enter":
		m.chosen = m.models[m.idx]
		return m, tea.Quit
	}
	return m, nil
}

func (m modelPicker) View() string {
	var b strings.Builder
	b.WriteString(tuiTitle.Render("Select a model") + "\n\n")
	// Длинный список не влезет в экран: показываем окно вокруг курсора.
	const h = 15
	off := 0
	if m.idx >= h {
		off = m.idx - h + 1
	}
	for i := off; i < min(off+h, len(m.models)); i++ {
		label := m.models[i]
		if m.models[i] == m.current {
			label += "  (current)"
		}
		row := "  " + label
		if i == m.idx {
			row = tuiCursor.Render("> " + label)
		}
		b.WriteString(row + "\n")
	}
	if len(m.models) > h {
		b.WriteString(tuiFaint.Render(fmt.Sprintf("\n%d of %d", m.idx+1, len(m.models))) + "\n")
	}
	b.WriteString(tuiFaint.Render("\nenter select   j/k move   q cancel"))
	return b.String()
}
