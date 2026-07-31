package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	files := []string{
		"D:\\Friday - Prototype\\go\\friday\\trading\\engine.go",
		"D:\\Friday - Prototype\\go\\trading\\bot.go",
		"D:\\Friday - Prototype\\go\\trading\\grid.go",
		"D:\\Friday - Prototype\\go\\trading\\backtest.go",
		"D:\\Friday - Prototype\\go\\trading\\blueguardian.go",
		"D:\\Friday - Prototype\\go\\trading\\strategy.go",
		"D:\\Friday - Prototype\\go\\trading\\strategy_regime.go",
		"D:\\Friday - Prototype\\go\\trading\\intelligence.go",
	}

	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			content, err := os.ReadFile(file)
			if err != nil {
				fmt.Printf("Error reading %s: %v\n", file, err)
				continue
			}

			original := string(content)
			fixed := strings.ReplaceAll(original, "github.com/friday-prototype/friday-go/execution", "github.com/friday-prototype/friday-go/internal/infrastructure/execution")

			if fixed != original {
				err := os.WriteFile(file, []byte(fixed), 0644)
				if err != nil {
					fmt.Printf("Error writing to %s: %v\n", file, err)
				} else {
					fmt.Printf("Fixed: %s\n", file)
				}
			}
		} else {
			fmt.Printf("File not found: %s\n", file)
		}
	}

	fmt.Println("Import path fixes complete!")
}