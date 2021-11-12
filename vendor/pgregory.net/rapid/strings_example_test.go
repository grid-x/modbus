// Copyright 2020 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// String generation depends on the Unicode tables, which change with Go versions:
// +build go1.14

package rapid_test

import (
	"fmt"
	"unicode"

	"pgregory.net/rapid"
)

func ExampleRune() {
	gen := rapid.Rune()

	for i := 0; i < 25; i++ {
		if i%5 == 0 {
			fmt.Println()
		} else {
			fmt.Print(" ")
		}
		fmt.Printf("%q", gen.Example(i))
	}
	// Output:
	// '\\' '\ufeff' '?' '~' '-'
	// '0' '$' '!' '`' '\ue05d'
	// '"' '&' '#' '\u0604' 'A'
	// '&' '茞' '@' '#' '|'
	// '⊙' '𝩔' '$' '҈' '\r'
}

func ExampleRuneFrom() {
	gens := []*rapid.Generator{
		rapid.RuneFrom([]rune{'A', 'B', 'C'}),
		rapid.RuneFrom(nil, unicode.Cyrillic, unicode.Greek),
		rapid.RuneFrom([]rune{'⌘'}, &unicode.RangeTable{
			R32: []unicode.Range32{{0x1F600, 0x1F64F, 1}},
		}),
	}

	for _, gen := range gens {
		for i := 0; i < 5; i++ {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Printf("%q", gen.Example(i))
		}
		fmt.Println()
	}
	// Output:
	// 'A' 'A' 'A' 'B' 'A'
	// 'Ͱ' 'Ѥ' 'Ͱ' 'ͱ' 'Ϳ'
	// '😀' '⌘' '😀' '😁' '😋'
}

func ExampleString() {
	gen := rapid.String()

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "\\߾⃝!/?Ⱥ֍"
	// "\u2006𑨷"
	// "?﹩\u0603ᾢ"
	// ".*%:<%৲"
	// ""
}

func ExampleStringOf() {
	gen := rapid.StringOf(rapid.RuneFrom(nil, unicode.Tibetan))

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "༁༭༇ཬ༆༐༖ༀྸ༁༆༎ༀ༁ཱི༂༨ༀ༂"
	// "༂༁ༀ༂༴ༀ༁ྵ"
	// "ༀ༴༁༅ན༃༁༎ྼ༄༽"
	// "༎༂༎ༀༀༀཌྷ༂ༀྥ"
	// ""
}

func ExampleStringN() {
	gen := rapid.StringN(5, 5, -1)

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "\\߾⃝!/"
	// "\u2006𑨷%\v\ufeff"
	// "?﹩\u0603ᾢÉ"
	// ".*%:<"
	// ":?\"~¤"
}

func ExampleStringOfN() {
	gen := rapid.StringOfN(rapid.ByteRange(65, 90), 5, 5, -1)

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "AXYHC"
	// "ESAAC"
	// "AUGWT"
	// "BRIOX"
	// "LYATZ"
}

func ExampleStringMatching() {
	gen := rapid.StringMatching(`\(?([0-9]{3})\)?([ .-]?)([0-9]{3})([ .-]?)([0-9]{4})`)

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "(532) 649-9610"
	// "901)-5783983"
	// "914.444.1575"
	// "(316 696.3584"
	// "816)0861080"
}

func ExampleSliceOfBytesMatching() {
	gen := rapid.SliceOfBytesMatching(`[CAGT]+`)

	for i := 0; i < 5; i++ {
		fmt.Printf("%q\n", gen.Example(i))
	}
	// Output:
	// "CCTTGAGAGCGATACGGAAG"
	// "GCAGAACT"
	// "AACCGTCGAG"
	// "GGGAAAAGAT"
	// "AGTG"
}
