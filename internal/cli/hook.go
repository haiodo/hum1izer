package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/haiodo/hum1izer/internal/config"
	"github.com/haiodo/hum1izer/internal/skill"
)

const hookUsage = `hum1izer hook - hooks for an agent, installed via install --hooks.

  hum1izer hook session-start   print the rule summary into the session context
  hum1izer hook post-edit       check the file the agent just edited
  hum1izer hook post-edit --file F   same, but path as a flag and plain text reply
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
	path, cwd, plain := "", "", false
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" && i+1 < len(args) {
			path, plain = args[i+1], true
			i++
		}
	}
	if !plain {
		var ev struct {
			CWD       string `json:"cwd"`
			ToolInput struct {
				FilePath string `json:"file_path"`
				Path     string `json:"path"`
			} `json:"tool_input"`
		}
		if err := json.NewDecoder(os.Stdin).Decode(&ev); err != nil {
			return 0
		}
		path, cwd = ev.ToolInput.FilePath, ev.CWD
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
	cmd := exec.Command(self, "--code", "--format", "md", "--limit", "5", "--commits", "0", path)
	cmd.Dir = cwd
	out, _ := cmd.Output()
	report := blocks(string(out))
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
