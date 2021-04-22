package tree

// experimenting with different ranking strategies/implementations
import (
	//	"errors"
	"fmt"
	"log"
)

type FightHist struct {
	// struct for our use with stormDB
	Time    int    `storm:"id"`    // unix timestamp used for ID
	P1name  string `storm:"index"` // winner's name
	P2name  string `storm:"index"` // loser's name
	P1total int    // total amnt bet on winner
	P2total int    // total amnt bet on loser
	Bet     int    // amnt we bet
	Profit  int    // amnt of profit made on that bet
	Winner  string // either "1" or "2"
	X       int    `storm:"index"` // ???
}
type Fightpair struct {
	Wname string
	Lname string
}

func (f *FightHist) GetPair() Fightpair {
	if f.Winner == "1" {
		return Fightpair{Wname: f.P1name, Lname: f.P2name}
	} else {
		return Fightpair{Wname: f.P1name, Lname: f.P2name}
	}
}

type tree struct {
	value    string
	children []*tree
}
type forest struct {
	Cache map[string]*tree // look up players' trees by name. Speeds up search.
	trees []*tree          // the base of our trees
}

func (f forest) String() string {
	res := ""
	if len(f.trees) >= 1 {
		res = f.trees[0].String()
		for _, child := range f.trees[1:] {
			if child != nil {
				res += "," + child.String()
			}
		}
	}
	return fmt.Sprintf(`[%s]`, res)
}

func (f *forest) AddFight(Wname string, Lname string) error {
	if Wname == "" || Lname == "" {
		return fmt.Errorf("InvalidArguments to AddFight")
	}
	// search forest and throw the match/branch up where it belongs
	wtree := f.Cache[Wname]

	if f.Cache[Lname] == nil { // Lname doesn't already have a tree
		f.Cache[Lname] = &tree{value: Lname}
	}

	if wtree != nil {
		if f.Cache[Lname].Find(Wname) != nil {
			// this condition can lead to a stack overflow in some rare edge cases.
			return fmt.Errorf("%s is already a descendent of %s. Discarding match.", Wname, Lname)
		}

		if wtree.Find(Lname) == nil { // Lname is not a descendent of Wname
			wtree.children = append(wtree.children, f.Cache[Lname])
		}
	} else { // Wname is fresh. Never seen.
		wtree := &tree{value: Wname}
		wtree.children = append(wtree.children, f.Cache[Lname])
		f.Cache[Wname] = wtree
		if f.trees[0].value == "" { // we're the first tree. initialization is weird.
			f.trees[0] = wtree
		} else {
			f.trees = append(f.trees, wtree)
		}
	}
	log.Printf("Fight %s, %s added.\n", Wname, Lname)
	return nil
}
func (t *tree) Find(name string) *tree { // better implementation possible? DFS currently.
	if t == nil {
		return nil // wtf?
	}
	if t.value == name {
		return t
	} else if t.children == nil {
		return nil
	} else {
		for _, child := range t.children {
			res := child.Find(name)
			if res != nil {
				return res
			}
		}
		return nil
	}
}
func (t *tree) Count() int {
	total := 1
	for _, c := range t.children {
		total += c.Count()
	}
	return total
}
func (f *forest) Predict(p1name string, p2name string) string {
	p1tree := f.Cache[p1name]
	p2tree := f.Cache[p2name]
	if p1tree != nil && p2tree != nil { // both exist.
		if p1tree.Find(p2name) != nil { // p1 likely to win.
			return p1name
		} else if p2tree.Find(p1name) != nil { // p2 likely to win.
			return p2name
		}
	}
	return "" // either it's a toss up, or we haven't see one of the players.
}
func (t *tree) Copy() *tree { // NOTE: Why is this a thing? Aren't references the idea?
	return &tree{
		children: t.children,
		value:    t.value,
	}
}

func (t tree) String() string {
	var childrenStr string
	if len(t.children) >= 1 {
		childrenStr = t.children[0].String()
		for _, child := range t.children[1:] {
			if child != nil {
				childrenStr += "," + child.String()
			}
		}
	}
	if childrenStr != "" {
		return fmt.Sprintf(`{"%s":[%s]}`, t.value, childrenStr)
	} else {
		return fmt.Sprintf(`"%s"`, t.value)
	}
}
func (t *tree) merge(partner *tree) {
	// merges two trees' children together
	// Possible memory leak issue?
	t.children = append(t.children, partner.children...)
	// filter t.children for uniqueness
	s := make(map[string]*tree)
	for _, c := range t.children {
		if s[c.value] != nil {
			// not unique. Merge current child with prev. seen.
			s[c.value].merge(c)
		} else {
			s[c.value] = c
		}
	}
	t.children = make([]*tree, 0)
	for _, v := range s {
		t.children = append(t.children, v)
	}
}
func BuildForest(waitstack []Fightpair) forest {
	var f forest
	f.Cache = make(map[string]*tree)
	f.trees = make([]*tree, 0)

	for len(waitstack) > 0 {
		if len(f.trees) == 0 {
			f.trees = append(f.trees, &tree{})
		}
		var match Fightpair
		switch len(waitstack) {
		case 0:
			break
		case 1:
			match, waitstack = waitstack[0], nil
		default:
			match, waitstack = waitstack[0], waitstack[1:]
		}
		//		var success bool
		err := f.AddFight(match.Wname, match.Lname)
		if err != nil {
			log.Printf("--- Error on Addfight(%s, %s) : %s", match.Wname, match.Lname, err)
		}
		// if !success { // didn't fit in any tree. build a fresh one.
		// 	var newtree tree // make a new tree to work on.
		// 	err := newtree.AddFight(match.Wname, match.Lname)
		// 	if err != nil {
		// 		break // invalid fight.
		// 	}
		// 	f.trees = append(f.trees, &newtree)
		// }
	}
	if len(f.trees) > 1 {
		// if there's more than one tree, lets have a look back through
		// to combine trees if a top-level player of a later tree is now child
		// of an earlier tree.
		var removeQ []int
		for i, cr := range f.trees[1:] { // offset to favor a large first tree.
			topplayer := cr.value
			var newParent *tree
			for j, tr := range f.trees {
				if i+1 == j { // not gonna be our own parent.
					continue
				}
				newParent = tr.Find(topplayer)
				if newParent != nil {
					break // we found our new parents!
				}
			}
			if newParent != nil {
				//	log.Printf("New parent of %v found: %v", cr, newParent)
				newParent.merge(cr)
				removeQ = append(removeQ, i+1)
			}
		}
		// reverse the indexes queue so we don't throw our numbers off
		// when we start clearing them out.
		for i := len(removeQ)/2 - 1; i >= 0; i-- {
			opp := len(removeQ) - 1 - i
			removeQ[i], removeQ[opp] = removeQ[opp], removeQ[i]
		}
		//		log.Println(removeQ)
		for _, i := range removeQ {
			// now delete all values f.trees[i] as they've been merged elsewhere.
			copy(f.trees[i:], f.trees[i+1:])
			f.trees[len(f.trees)-1] = nil
			f.trees = f.trees[:len(f.trees)-1]
		}
		return f
	}

	manualcount := 0
	for _, r := range f.trees {
		manualcount += r.Count()
	}
	log.Printf("%d Trees built with %d players counted @ %d branches", len(f.trees), len(f.Cache), manualcount)
	return f
}

func ForestFromQuery(fightsQuery []FightHist) forest {
	waitstack := []Fightpair{}
	for _, hist := range fightsQuery {
		waitstack = append(waitstack, hist.GetPair())
	}
	return BuildForest(waitstack)
}

// func main() {
// 	db, _ := storm.Open("./storm.db")
// 	defer db.Close()

// 	var fightsQuery []FightHist
// 	err := db.All(&fightsQuery)
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	PrFromQuery(fightsQuery)
// }
