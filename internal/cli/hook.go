package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/skill"
)

const hookUsage = `hum1izer hook - hooks for an agent, installed via install --hooks.

  hum1izer hook session-start   print the rule summary into the session context
  hum1izer hook post-edit       check the file the agent just edited
  hum1izer hook post-edit --file F   same, but path as a flag and plain text reply
  hum1izer hook post-edit --force-show   repeat blocks already shown this session
  hum1izer hook session-start --text rule summary without the JSON wrapper

Both commands are meant to run from an agent, not by hand: post-edit reads a
JSON event from stdin and stays silent if there are no findings. Agents
without hook settings (opencode, pi) call the same commands from the plugin
and get plain text back.
`

func sessionText() string {
	return "hum1izer: comment rules for this session.\n\n" + skill.Rules()
}

// hookOut - ответ хука. additionalContext попадает в контекст модели и не показывается
// пользователю.
type hookOut struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func emit(event, text string) {
	var out hookOut
	out.HookSpecificOutput.HookEventName = event
	out.HookSpecificOutput.AdditionalContext = text
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

func runHook(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, hookUsage)
		return 2
	}
	switch args[0] {
	case "session-start":
		if len(args) > 1 && args[1] == "--text" {
			fmt.Println(sessionText())
			return 0
		}
		emit("SessionStart", sessionText())
		return 0
	case "post-edit":
		return hookPostEdit(args[1:])
	}
	fmt.Fprint(os.Stderr, hookUsage)
	return 2
}

// blocks вырезает из отчёта сами находки: шапка "как править" одна на все
// прогоны, агент её уже знает из скилла. Нет заголовков блоков - нет находок.
func blocks(report string) string {
	if i := strings.Index("\n"+report, "\n## "); i >= 0 {
		return strings.TrimSpace(("\n" + report)[i:])
	}
	return ""
}

// hookPostEdit проверяет один файл после правки. Claude Code шлёт событие в stdin и ждёт JSON,
// остальные агенты зовут --file и печатают ответ сами.
func hookPostEdit(args []string) int {
	path, cwd, session, plain, force := "", "", "", false, false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--file" && i+1 < len(args):
			path, plain = args[i+1], true
			i++
		case args[i] == "--force-show":
			force = true
		}
	}
	if !plain {
		var ev struct {
			SessionID string `json:"session_id"`
			CWD       string `json:"cwd"`
			ToolInput struct {
				FilePath string `json:"file_path"`
				Path     string `json:"path"`
			} `json:"tool_input"`
		}
		if err := json.NewDecoder(os.Stdin).Decode(&ev); err != nil {
			return 0
		}
		path, cwd, session = ev.ToolInput.FilePath, ev.CWD, ev.SessionID
		if path == "" {
			path = ev.ToolInput.Path
		}
	}
	if path == "" || config.LangOf(path) == "" {
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		return 0
	}
	abs := path
	if !filepath.IsAbs(abs) && cwd != "" {
		abs = filepath.Join(cwd, abs)
	}
	lines, tracked := changedLines(abs)
	if tracked && lines == "" {
		return 0
	}
	// Лимит после отсева показанного: иначе старые блоки вытеснят новые из топа.
	check := []string{"--code", "--format", "md", "--limit", "0", "--commits", "0"}
	if lines != "" {
		check = append(check, "--lines", lines)
	}
	cmd := exec.Command(self, append(check, path)...)
	cmd.Dir = cwd
	out, _ := cmd.Output()
	report := blocks(string(out))
	if !force {
		report = unseen(report, session, abs)
	}
	if report == "" {
		return 0
	}
	text := "hum1izer found the following in " + path + ". Address the findings: fix the block " +
		"or mark `fix --block <id> --keep` if the rule fired for nothing.\n\n" + report
	if plain {
		fmt.Println(text)
		return 0
	}
	emit("PostToolUse", text)
	return 0
}

// changedLines - строки, отличные от HEAD, для --lines: иначе хук приносил старые находки всего
// файла. tracked=false - файла нет в git, проверяется целиком.
func changedLines(path string) (lines string, tracked bool) {
	dir := filepath.Dir(path)
	if exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", path).Run() != nil {
		return "", false
	}
	out, err := exec.Command("git", "-C", dir, "diff", "-U0", "--no-color", "--no-ext-diff",
		"HEAD", "--", path).Output()
	if err != nil {
		return "", false
	}
	var spans []string
	for _, l := range strings.Split(string(out), "\n") {
		// @@ -a,b +c,d @@: новые строки c..c+d-1, d по умолчанию 1.
		f := strings.Fields(l)
		if len(f) < 3 || f[0] != "@@" {
			continue
		}
		a, b, hasCount := strings.Cut(strings.TrimPrefix(f[2], "+"), ",")
		from, err := strconv.Atoi(a)
		if err != nil {
			continue
		}
		n := 1
		if hasCount {
			if n, err = strconv.Atoi(b); err != nil {
				continue
			}
		}
		if n == 0 {
			continue
		}
		spans = append(spans, fmt.Sprintf("%d-%d", from, from+n-1))
	}
	return strings.Join(spans, ","), true
}

// blockHead - шапка блока в md-отчёте: строка "## место" и за ней "block <hash> |".
var blockHead = regexp.MustCompile(`(?m)^## .*\nblock ([0-9a-f]+) \|`)

// unseen отсекает блоки, уже показанные в сессии для файла; hash зависит от текста, поправленный
// блок покажется снова. Без session_id (opencode, pi) кеш общий.
func unseen(report, session, file string) string {
	heads := blockHead.FindAllStringSubmatchIndex(report, -1)
	if len(heads) == 0 {
		return report
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return report
	}
	if session == "" {
		session = "default"
	}
	// Файлы сессий не чистятся, по килобайту на сессию; чистить по mtime, если разрастётся.
	cache := filepath.Join(dir, "hum1izer", "shown", filepath.Base(session)+".json")
	shown := map[string][]string{}
	if b, err := os.ReadFile(cache); err == nil {
		_ = json.Unmarshal(b, &shown)
	}
	seen := map[string]bool{}
	for _, h := range shown[file] {
		seen[h] = true
	}
	var out []string
	for i, m := range heads {
		end := len(report)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		hash := report[m[2]:m[3]]
		if seen[hash] || len(out) == 5 {
			continue
		}
		seen[hash] = true
		shown[file] = append(shown[file], hash)
		out = append(out, strings.TrimSpace(report[m[0]:end]))
	}
	if len(out) == 0 {
		return ""
	}
	if b, err := json.Marshal(shown); err == nil && os.MkdirAll(filepath.Dir(cache), 0o755) == nil {
		_ = os.WriteFile(cache, b, 0o644)
	}
	return strings.Join(out, "\n\n")
}
