package main

// experimenting with different ranking strategies/implementations
import (
	"errors"
	"fmt"
	"github.com/asdine/storm"
	"log"
)

type fightHist struct {
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
	wname string
	lname string
}

func (f *fightHist) getPair() Fightpair {
	if f.Winner == "1" {
		return Fightpair{wname: f.P1name, lname: f.P2name}
	} else {
		return Fightpair{wname: f.P1name, lname: f.P2name}
	}
}

type tree struct {
	value    string
	children []*tree
}
type forest struct {
	trees map[string]*tree
}

func (t *tree) AddFight(wname string, lname string) error {
	if wname == "" || lname == "" {
		return fmt.Errorf("InvalidArguments to AddFight")
	}
	// search tree and throw the match up where it belongs
	if t.value == "" {
		t.value = wname
		t.children = append(t.children, &tree{value: lname})
		return nil
	}
	if wtree := t.Find(wname); wtree != nil {
		ltree := wtree.Find(lname)
		if ltree == nil { // lname is not a descendent of wname
			wtree.children = append(wtree.children, &tree{value: lname})
		} else { // lname is already a descendent of wname.
			return nil
		}
	} else if ltree := t.Find(lname); ltree != nil {
		wtree := &tree{
			children: []*tree{ltree.Copy()},
			value:    wname,
		}
		ltree.children = wtree.children
		ltree.value = wtree.value
		return nil
	}
	//	log.Printf(t.repr())
	return errors.New("names " + wname + " " + lname + " not found in tree")
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
func Predict(pr []*tree, p1name string, p2name string) string {
	for _, t := range pr {
		p1tree := t.Find(p1name)
		p2tree := t.Find(p2name)
		if p1tree != nil && p2tree != nil { // both exist.
			if p1tree.Find(p2name) != nil { // p1 likely to win.
				return p1name
			} else if p2tree.Find(p1name) != nil { // p2 likely to win.
				return p2name
			}
		}
	}
	return "" // either it's a toss up, or we haven't see one of the players.
}
func (t *tree) Copy() *tree {
	return &tree{
		children: t.children,
		value:    t.value,
	}
}

func (t *tree) String() string {
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
func BuildTree(waitstack []Fightpair) []*tree {
	var pr []*tree
	players := make(map[string]bool)

	for len(waitstack) > 0 {
		if len(pr) == 0 {
			pr = append(pr, &tree{})
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
		players[match.wname] = true
		players[match.lname] = true

		var success bool
		for _, cr := range pr {
			err := cr.AddFight(match.wname, match.lname)
			if err == nil {
				success = true
				break // success! It fits!
			}
		}
		if !success { // didn't fit in any tree. build a fresh one.
			var newtree tree // make a new tree to work on.
			err := newtree.AddFight(match.wname, match.lname)
			if err != nil {
				break // invalid fight.
			}
			pr = append(pr, &newtree)
		}
	}
	if len(pr) > 1 {
		// if there's more than one tree, lets have a look back through
		// to combine trees if a top-level player of a later tree is now child
		// of an earlier tree.
		var removeQ []int
		for i, cr := range pr[1:] { // offset to favor a large first tree.
			topplayer := cr.value
			var newParent *tree
			for j, tr := range pr {
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
			// now delete all values pr[i] as they've been merged elsewhere.
			copy(pr[i:], pr[i+1:])
			pr[len(pr)-1] = nil
			pr = pr[:len(pr)-1]
		}
	}

	manualcount := 0
	for _, r := range pr {
		manualcount += r.Count()
	}
	log.Printf("%d Trees built with %d players counted @ %d branches", len(pr), len(players), manualcount)
	return pr
}

func PrFromQuery(fightsQuery []fightHist) []*tree {
	waitstack := []Fightpair{}
	for _, hist := range fightsQuery {
		waitstack = append(waitstack, hist.getPair())
	}
	return BuildTree(waitstack)
}
func main() {
	db, _ := storm.Open("./storm.db")
	defer db.Close()

	var fightsQuery []fightHist
	err := db.All(&fightsQuery)
	if err != nil {
		log.Fatal(err)
	}
	PrFromQuery(fightsQuery)
}
