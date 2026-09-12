package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Ключ живёт в файле пользователя, а не в репозитории, и файл должен быть
// закрыт от посторонних: 0600 здесь часть требования, а не украшение.
func TestUserConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if got.LLM.Key != "" {
		t.Error("на первом запуске файла нет, а ключ откуда-то взялся")
	}

	want := User{LLM: LLM{BaseURL: "http://localhost:11434/v1/", Key: "sk-secret", Model: "glm-4.7"}}
	path, err := SaveUser(want)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "hum1izer", "config.yaml") {
		t.Errorf("путь %q", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("права %v, ожидались 0600", st.Mode().Perm())
	}

	got, err = LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("прочитано %+v, записано %+v", got, want)
	}
}
