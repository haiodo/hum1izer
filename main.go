// Command hum1izer проверяет текст, комментарии в коде и сообщения коммитов
// на следы нейросети, канцелярит и штампы.
package main

import (
	"os"

	"github.com/haiodo/hum1izer/internal/cli"
)

func main() { os.Exit(cli.Run()) }
