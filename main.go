// Command reporepo は GitHub リポジトリを AI で要約・解説する TUI アプリのエントリポイント。
package main

import (
	"os"

	"github.com/issy20/reporepo/cmd"
)

func main() {
	os.Exit(run(cmd.Execute))
}

func run(execute func() error) int {
	if err := execute(); err != nil {
		return 1
	}
	return 0
}
