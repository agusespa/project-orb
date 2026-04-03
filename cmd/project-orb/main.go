package main

import "fmt"

func main() {
	p := newProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("failed to start UI: %v\n", err)
	}
}
