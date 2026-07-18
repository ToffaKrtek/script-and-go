package main

import (
	"fmt"
	"os"

	"github.com/ToffaKrtek/script-and-go/internal/run"
	"github.com/ToffaKrtek/script-and-go/internal/yml"
	"github.com/manifoldco/promptui"
)

func main() {
	scripts, err := yml.LoadScripts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка загрузки скриптов: %v\n", err)
		os.Exit(1)
	}
	if len(scripts) == 0 {
		fmt.Println("Нет сценариев в ~/.script-and-go")
		os.Exit(0)
	}
	names := make([]string, len(scripts))
	for i, s := range scripts {
		names[i] = s.Name
	}
	prompt := promptui.Select{
		Label: "Выберите сценарий",
		Items: names,
	}
	idx, _, err := prompt.Run()
	if err != nil {
		fmt.Printf("Выход: %v\n", err)
		os.Exit(0)
	}
	selected := scripts[idx]
	fmt.Printf("\n=== Запуск сценария: %s ===\n", selected.Name)
	if err := run.RunScript(selected); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка выполнения сценария: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Сценарий завершен ===")
}
