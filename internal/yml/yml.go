package yml

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ToffaKrtek/script-and-go/types"
	"gopkg.in/yaml.v3"
)

func LoadScripts() ([]types.Script, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".script-and-go")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("директория %s не существует", dir)
		}
		return nil, err
	}
	var scripts []types.Script
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка чтения файла %s: %v\n", path, err)
			continue
		}
		var s types.Script
		if err := yaml.Unmarshal(data, &s); err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка парсинга %s: %v\n", path, err)
			continue
		}
		scripts = append(scripts, s)
	}
	return scripts, nil
}
