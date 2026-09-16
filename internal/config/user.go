package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// User - настройки человека, а не проекта: адрес API, ключ, модель. Ключу в
// репозитории не место, поэтому файл в домашнем каталоге и с правами 0600.
type User struct {
	LLM LLM `yaml:"llm,omitempty"`
}

// UserPath - $XDG_CONFIG_HOME/hum1izer/config.yaml, иначе ~/.config/hum1izer.
// Тот же путь на всех системах: так его проще назвать в документации.
func UserPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hum1izer", "config.yaml"), nil
}

// LoadUser: отсутствие файла не ошибка, это первый запуск.
func LoadUser() (User, error) {
	path, err := UserPath()
	if err != nil {
		return User{}, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return User{}, nil
	}
	if err != nil {
		return User{}, err
	}
	var u User
	if err := yaml.Unmarshal(raw, &u); err != nil {
		return User{}, fmt.Errorf("%s: %w", path, err)
	}
	return u, nil
}

func SaveUser(u User) (string, error) {
	path, err := UserPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	raw, err := yaml.Marshal(u)
	if err != nil {
		return "", err
	}
	head := "# hum1izer settings for this user: API address, key and model.\n" +
		"# Not for the repo - the key lives here.\n\n"
	if err := os.WriteFile(path, append([]byte(head), raw...), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
