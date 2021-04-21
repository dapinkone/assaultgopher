package main

import (
	"fmt"
	"strings"
	"testing"
)

func fightPairFromStr(s string) Fightpair {
	// Note: real data has spaces in the names. fix this?
	tmp := strings.Split(s, " ")
	w, l := tmp[0], tmp[1]
	return Fightpair{wname: w, lname: l}
}

type test struct {
	name      string
	fightstrs []string
	want      string
}

func (t *test) toStack() []Fightpair {
	var waitstack []Fightpair
	for _, fight := range t.fightstrs {
		waitstack = append(waitstack, fightPairFromStr(fight))
	}
	return waitstack
}

var tests = []test{
	{"Multiple levels", []string{"A B", "B C"}, `[{"A":[{"B":["C"]}]}]`},
	{"Multiple children", []string{"A B", "A C", "A D"}, `[{"A":["B","C","D"]}]`},
	{"Data with gaps", []string{"A B", "C D"}, `[{"A":["B"]} {"C":["D"]}]`},
	{"Closing data gaps",
		[]string{"A B", "C D", "B C"},
		`[{"A":[{"B":[{"C":["D"]}]}]}]`,
	},
	{"No Duplicate Children", []string{"A B", "A B"}, `[{"A":["B"]}]`},
	{"Tree Branch Sharing",
		[]string{"A C", "C F", "D C"}, `[{"A":[{"C":["F"]}]},{"D":[{"C":["F"]}]}]`,
	},
}

func TestTreeCreation(t *testing.T) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf("%v", BuildTree(tt.toStack()))
			if got != tt.want {
				t.Errorf("wanted %s\tgot %s", tt.want, got)
			}
		})
	}
}

func TestTreeMerge(t *testing.T) {
	t.Run("Merging trees", func(t *testing.T) {
		var a, b tree
		a.AddFight("A", "B")
		b.AddFight("A", "C")
		a.merge(&b)
		got := fmt.Sprintf("%s", a.String())
		optionA := `{"A":["C","B"]}`
		optionB := `{"A":["B","C"]}`
		if got != optionA && got != optionB {
			t.Errorf("wanted %s\tgot %s", optionA, got)
		}
	})

}
