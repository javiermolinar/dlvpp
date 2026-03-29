package main

import "fmt"

func add(a, b int) int {
	sum := a + b
	return sum
}

func main() {
	message := "hello delve world"
	// breakpoint
	total := add(20, 22)
	fmt.Println(message)
	fmt.Println("total:", total)
}
