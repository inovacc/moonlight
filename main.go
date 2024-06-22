package main

import (
	"fmt"
	"sort"
	"strings"
)

//TIP To <b>Run</b> code, right-click the code and select <b>Run</b>. Alternatively, click
// the <icon src="AllIcons.Actions.Execute"/> icon in the gutter and select the <b>Run</b> menu item from here.

func main() {
	//TIP Press <shortcut actionId="ShowIntentionActions"/> when your caret is at the underlined or highlighted text
	// to see how GoLand suggests fixing it.
	s := "gopher"
	fmt.Println("Hello and welcome, %s!", s)

	//TIP The IDE supports full-line code completion for Go. Full-line code completion is AI-powered and runs locally
	// without sending any data over the internet. And it is completely free. Place the caret at the end of the 'letters'
	// slice and press <shortcut actionId="EditorEnter"/>, type 'letters', and wait for a couple of seconds. To accept
	// the suggestion, press <shortcut actionId="Tab"/>.
	letters := []string{"C", "Q", "R", "A", "U"}

	for i := 1; i <= 5; i++ {
		//TIP Press <shortcut actionId="Debug"/> to start debugging your code. We have set one <icon src="AllIcons.Debugger.Db_set_breakpoint"/> breakpoint
		// for you, but you can always add more by pressing <shortcut actionId="ToggleLineBreakpoint"/>.
		fmt.Println("i =", 100/i)
	}
	defer fmt.Println(letters)
}

func sortLetters(l []string) {
	sort.Strings(l)
	fmt.Printf(strings.Join(l, " "))
}

//TIP See GoLand help at <a href="https://www.jetbrains.com/help/go/">jetbrains.com/help/go/</a>.
// Also, you can try interactive lessons for Go by selecting 'Help | Learn IDE Features' from the main menu.
