package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/haiodo/hum1izer/internal/code"
)

// Хук смотрит только строки, изменённые против HEAD: старые находки файла агент
// не трогал, и чинить их посреди чужой задачи он не должен.
func TestChangedLines(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	file := filepath.Join(dir, "a.go")
	write := func(s string) {
		if err := os.WriteFile(file, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("package a\n")
	if _, tracked := changedLines(file); tracked {
		t.Error("файл вне git должен проверяться целиком")
	}
	git("init", "-q")
	if _, tracked := changedLines(file); tracked {
		t.Error("неотслеживаемый файл должен проверяться целиком")
	}
	write("package a\n\n// old\nfunc A() {}\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	if lines, tracked := changedLines(file); !tracked || lines != "" {
		t.Errorf("без правок: %q, %v", lines, tracked)
	}
	write("package a\n\n// old\nfunc A() {}\n\n// new\nfunc B() {}\n")
	if lines, _ := changedLines(file); lines != "5-7" {
		t.Errorf("изменённые строки %q, ожидалось 5-7", lines)
	}

	items := []code.Item{{Start: 3, End: 3}, {Start: 6}}
	got, err := touching(items, "5-7")
	if err != nil || len(got) != 1 || got[0].Start != 6 {
		t.Errorf("touching: %v, %v", got, err)
	}
}

// Один и тот же блок в сессии показывается один раз, поправленный - снова.
func TestUnseenOncePerSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	block := func(line int, hash string) string {
		return fmt.Sprintf("## a.go:%d\nblock %s | `go` | comment | weight 1\n\n- **rule** (line %d)", line, hash, line)
	}
	report := block(3, "aa") + "\n\n" + block(9, "bb")

	if got := unseen(report, "s1", "/a.go"); got != report {
		t.Fatalf("первый показ должен пройти целиком:\n%s", got)
	}
	if got := unseen(report, "s1", "/a.go"); got != "" {
		t.Errorf("повтор в той же сессии:\n%s", got)
	}
	if got := unseen(block(3, "aa")+"\n\n"+block(9, "cc"), "s1", "/a.go"); got != block(9, "cc") {
		t.Errorf("должен остаться только изменённый блок:\n%s", got)
	}
	if got := unseen(report, "s2", "/a.go"); got != report {
		t.Errorf("новая сессия видит всё заново:\n%s", got)
	}
}
