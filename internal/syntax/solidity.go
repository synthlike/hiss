package syntax

import "fmt"

// Solidity is C-like, with strings that stay on their line and a family of
// sized types.
var Solidity = &Lang{Name: "solidity", LineComment: "//", BlockComment: [2]string{"/*", "*/"}, Quotes: "\"'",
	Keywords: solidityWords()}

func solidityWords() map[string]bool {
	m := words(`abstract anonymous as assembly assert break calldata catch constant constructor
		continue contract delete do else emit enum error event external fallback false for from
		function if immutable import indexed interface internal is let library mapping memory
		modifier new override payable pragma private public pure receive require return returns
		revert storage struct super this transient true try type unchecked using view virtual while
		address bool bytes int string uint wei gwei ether`)
	for bits := 8; bits <= 256; bits += 8 {
		m[fmt.Sprint("int", bits)], m[fmt.Sprint("uint", bits)] = true, true
		m[fmt.Sprint("bytes", bits/8)] = true
	}
	return m
}
