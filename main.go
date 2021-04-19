package main

// credits:
// https://blog.alexellis.io/golang-json-api-client/

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/asdine/storm"
	"github.com/gocolly/colly/v2"
	gosocketio "github.com/graarh/golang-socketio"
	"github.com/graarh/golang-socketio/transport"
)

func strToInt(s string) int {
	clean := strings.Replace(s, ",", "", -1)
	if clean != "" {
		i, _ := strconv.Atoi(clean)
		return i
	} else {
		return 0
	}
}

func Index(arr []string, item string) int {
	for ind, j := range arr {
		if j == item {
			return ind
		}
	}
	return -1
}

func Insert(s []string, k int, vs ...string) []string {
	// credit https://github.com/golang/go/wiki/SliceTricks
	if n := len(s) + len(vs); n <= cap(s) {
		s2 := s[:n]
		copy(s2[k+len(vs):], s[k:])
		copy(s2[k:], vs)
		return s2
	}
	s2 := make([]string, len(s)+len(vs))
	copy(s2, s[:k])
	copy(s2[k:], vs)
	copy(s2[k+len(vs):], s[k:])
	return s2
}

////////////////////////////////////////
// some credit for the below to omotto @
// https://stackoverflow.com/questions/43616676/moving-an-slice-item-from-one-position-to-another-in-go

func remove(array []string, index int) []string {
	return append(array[:index], array[index+1:]...)
}

func move(array []string, srcIndex int, dstIndex int) []string {
	value := array[srcIndex]
	return Insert(remove(array, srcIndex), dstIndex, value)
}

// </credit>
/////////////////////////////////////////

// get game state json from
// https://www.saltybet.com/state.json
// it's used over here https://www.saltybet.com/j/www-cdn-jtvnw-x.js
type GameState struct {
	P1name  string // player 1/red's name
	P2name  string // player 2/blue's name
	P1total string // total $ on red, w/commas
	P2total string // total $ on blue, w/commas
	Status  string
	// betting open? "1", "2", "open" "locked"
	//  1		-p1's win.
	//  2		-p2's win.
	//  open  	-betting open.
	//  locked	-betting closed.

	Alert string
	// Tournament mode start!
	// Exhibition mode start!
	// ?? unknown
	// usually blank
	X         int    `json:"number"`
	Remaining string // String including the # of matches until the next tournament
}

func (g *GameState) calcOdds(player string) float64 { // calculate the odds for a player
	switch player {
	case "1":
		return (float64(strToInt(g.P2total)) / float64(strToInt(g.P1total)))
	case "2":
		return (float64(strToInt(g.P1total)) / float64(strToInt(g.P2total)))
	default:
		return 0
	}
}
func (g *GameState) calcProfit(wager int, player string) int {
	p1float := float64(strToInt(g.P1total))
	p2float := float64(strToInt(g.P2total))
	if g.Status == "1" && player == g.P1name { // did red win?
		return int(math.Ceil(float64(wager) / p1float * p2float))
	} else if g.Status == "2" && player == g.P2name {
		return int(math.Ceil(float64(wager) / p2float * p1float))
	} else {
		//		log.Printf("%s bad bet.", player)
		return -1 * wager
	}
}

func (g GameState) String() string {
	/* implements the stringer interface on GameState, so we can use standard print stuff on it
	   with our desired custom output */
	odds := g.calcOdds("2")
	betstatus := func() string {
		switch g.Status {
		case "1":
			return fmt.Sprintf("'%s' Wins", g.P1name)
		case "2":
			return fmt.Sprintf("'%s' Wins", g.P2name)
		default:
			return "Bets are " + g.Status
		}
	}()
	a := fmt.Sprintf("'%v' '%v' (%.1f:1) $%v $%v %v  x:%v\t%v", // TODO: fix this format string.
		g.P1name, g.P2name, odds, g.P1total, g.P2total, betstatus, g.X, g.Remaining,
	)
	if g.Alert != "" {
		a += fmt.Sprintf(" Alert: %s", g.Alert)
	}
	return a
}

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

func diaf(err error) {
	if err != nil {
		log.Fatal(err)

	}
}
func main() {

	// connect to the local database.
	db, _ := storm.Open("./storm.db")
	defer db.Close()

	ranks := []string{} // players ranking
	var fightsQuery []fightHist
	err := db.All(&fightsQuery)
	diaf(err)
	seen := make(map[string]bool)

	recordFight := func(p1name string, p2name string, result string) {
		var winner string
		var loser string
		if result == "1" {
			winner, loser = p1name, p2name
		} else {
			winner, loser = p2name, p1name
		}
		if seen[winner] && seen[loser] {
			if Index(ranks, winner) < Index(ranks, loser) {
				// move the winner to be past the loser.
				ranks = move(ranks, Index(ranks, winner), Index(ranks, loser))
			}
			// elsewise, the order is already fine.
		} else if seen[winner] {
			// haven't seen loser. loser goes to bottom rank
			// until they win something.
			ranks = append([]string{loser}, ranks...)
		} else if seen[loser] {
			ranks = Insert(ranks, Index(ranks, loser), winner)
		} else {
			// both new fighters. Throw them on the bottom.
			// They'll work their way up eventually.
			ranks = append([]string{loser, winner}, ranks...)
		}
		seen[winner] = true
		seen[loser] = true
	}

	predict := func(p1 string, p2 string) string {
		// check our player ranking for who's "on top"
		p1rank := Index(ranks, p1)
		p2rank := Index(ranks, p2)
		log.Printf("Predict: %s(%d) vs %s(%d)", p1, p1rank, p2, p2rank)
		if p1rank == -1 || p2rank == -1 { // not sure. New players.
			return ""
		}
		if p1rank >= p2rank {
			return p1
		} else {
			return p2
		}
	}
	var betHist []bool
	var goodPredicts int
	var totalPredicts int
	for _, fight := range fightsQuery {
		// cycle through some fights we saw previously to build of a library.
		predictedWinner := predict(fight.P1name, fight.P2name)
		var wname string
		switch fight.Winner {
		case "1":
			wname = fight.P1name
		case "2":
			wname = fight.P2name
		}
		if predictedWinner != "" {
			betHist = append(betHist, predictedWinner == wname)
		}
		// display a count of our accuracy over prediction history
		recordFight(fight.P1name, fight.P2name, fight.Winner)
	}
	totalPredicts = len(betHist)

	for _, item := range betHist {
		if item == true {
			goodPredicts++
		}
	}

	accuracy := float64(goodPredicts) / float64(totalPredicts) * 100
	log.Printf("Ranks built from %d data entries. %d players.\nBacklog expects %0.1f accuracy", len(fightsQuery), len(ranks), accuracy)

	// step 1: login
	c := colly.NewCollector()
	c.AllowURLRevisit = true

	// open up the auth.txt to get our username/password. TODO: need consult on more secure method.
	file, err := os.OpenFile("./auth.txt", os.O_RDONLY, 0644)
	diaf(err)
	defer file.Close()
	authbytes, err := ioutil.ReadAll(file)
	tmp := strings.Split(string(authbytes), "\n")
	email, pword := tmp[0], tmp[1]
	err = c.Post(
		"https://www.saltybet.com/authenticate?signin=1",
		map[string]string{
			"email":        email,
			"pword":        pword,
			"authenticate": "signin",
		},
	)
	diaf(err)

	// attach callbacks after login
	c.OnResponse(func(r *colly.Response) {
		log.Printf("rcvd %d %s", r.StatusCode, r.Request.URL)
	})

	currentBal := 0

	c.OnHTML("#b", func(e *colly.HTMLElement) { // initial value of our balance, from the main page.
		if e.Attr("value") != " " && e.Text != strconv.Itoa(currentBal) {
			currentBal = strToInt(e.Attr("value"))
			log.Println("Page Bal:", currentBal)
		}
	})
	uid := ""
	c.OnHTML("#u", func(e *colly.HTMLElement) { // this only shows up on startup.
		if e.Attr("value") != uid {
			uid = e.Attr("value")
			log.Println("Uid:", uid)
		}
	})

	// need to log in and hit up the main page to get our balance and get our session ids to bet.
	err = c.Visit("https://www.saltybet.com/")
	diaf(err)

	// checking cookies for __cfduid, PHPSESSID
	for _, cookie := range c.Cookies("https://www.saltybet.com/authenticate?signin=1") {
		log.Println(cookie.Name)
	}

	t := func() string { // time in milliseconds, as a string. used by the API for something. who knows.
		return strconv.Itoa(int(time.Now().UnixNano()) / int(time.Millisecond))
	}

	lastState := GameState{}
	wager := 0
	var predictedWinner string
	updateState := func() {
		// fetch json/gamestate
		httpClient := http.Client{Timeout: time.Second * 10}

		req, err := http.NewRequest(
			http.MethodGet,
			"https://www.saltybet.com/state.json",
			nil,
		)
		if err != nil {
			log.Println("-- ERROR: ", err)
			return // bail out. We'll update next time.
		}

		res, err := httpClient.Do(req)
		if err != nil {
			log.Println("-- ERROR: ", err)
			return // bail out. We'll update next time.
		}

		if res.Body != nil {
			defer res.Body.Close()
		}

		body, err := ioutil.ReadAll(res.Body)
		diaf(err)

		var newState GameState
		err = json.Unmarshal(body, &newState)
		if lastState.Status == newState.Status {
			return // duplicate detected. bail out. We shouldn't see two statuses of the same.
		}
		diaf(err)
		lastState = newState
		log.Println(lastState)

		switch lastState.Status {
		case "open":

			// place bet!
			// post request to /ajax_place_bet.php
			wager = currentBal / 10
			var selectedPlayer string
			predictedWinner = predict(lastState.P1name, lastState.P2name)
			if predictedWinner == "" { // Unkown player.
				// We don't bet unless we've seen the player before.
				// Otherwise we tend to leak money like a sieve.
				log.Println("Unknown player, Not betting.")
				return
			}
			if predictedWinner == lastState.P1name {
				selectedPlayer = "player1"
			} else {
				selectedPlayer = "player2"
			}

			c.Post(
				"https://www.saltybet.com/ajax_place_bet.php",
				map[string]string{
					"selectedplayer": selectedPlayer,
					"wager":          strconv.Itoa(wager),
				},
			)
			log.Printf("Bet placed for %d on %s!", wager, predictedWinner)
			break

		case "locked":
			// bets are now closed.
		case "1", "2": // fight's over.
			var winnerName string
			switch lastState.Status {
			case "1":
				winnerName = lastState.P1name
			case "2":
				winnerName = lastState.P2name
			}
			estProfit := 0
			if Index(ranks, lastState.P1name) != -1 && Index(ranks, lastState.P2name) != -1 {
				// we've seen these guys before, so we would have bet.
				betHist = append(betHist, predictedWinner == winnerName)
				estProfit = lastState.calcProfit(wager, predictedWinner)
			}
			err = db.Save(&fightHist{
				Time:    int(time.Now().Unix()),
				P1name:  lastState.P1name,
				P2name:  lastState.P2name,
				P1total: strToInt(lastState.P1total),
				P2total: strToInt(lastState.P2total),
				Winner:  lastState.Status,
				Bet:     wager, // TODO: put the bet data in when you calculate profits later.
				Profit:  estProfit,
				X:       lastState.X,
			})
			if err != nil {
				log.Println(err)
			}

			recordFight(lastState.P1name, lastState.P2name, lastState.Status)
			// display a count of our accuracy over prediction history
			//				totalPredicts = len(betHist)

			// we want to take a sliding estimate of our accuracy.
			var histSlice []bool

			if len(betHist) > 50 {
				histSlice = betHist[len(betHist)-50:]
			} else {
				histSlice = betHist
			}

			goodPredicts = 0.0
			for _, item := range histSlice {
				if item == true {
					goodPredicts++
				}
			}

			accuracy := float64(goodPredicts) * 100 / float64(len(histSlice))

			currentBal = estProfit + currentBal
			log.Printf("Balance updated: %d Change: %d @ last %d bets had %0.2f%% acc %d known players\n\n",
				currentBal, estProfit, len(histSlice),
				accuracy, len(ranks))

		}
	}

	updateState() // initialize our gamestate.
	// for our main event loop, we're gonna connect a websocket
	// then we do stuff when the socket passes us data.

	//	wsurl := "www.saltybet.com:2096/socket.io/?EIO=3&transport=websocket&t=" + t + "-0"

	wsurl := "wss://www.saltybet.com:2096/socket.io/?EIO=3&transport=websocket&t=" + t() + "-0"
	log.Println(wsurl)
	ws, err := gosocketio.Dial(
		wsurl,
		// EIO=3 ?
		// transport=websocket or polling

		// sid=....? found in cookie's io var?
		// socket receive: type "open", data "{"sid":"36Q3vIX320gzWmcFBMtO","upgrades":["websocket"],"pingInetrval":25000,"pingTimeout":60000}" +1ms

		// t= unixtime in ms
		// ----
		// do I need to pass in the colly cookie for PHPSESSID, etc?
		transport.GetDefaultWebsocketTransport(),
	)
	defer ws.Close()

	ws.On("message", func(data *gosocketio.Channel) {
		updateState()
	})

	websockfinished := make(chan bool)
	ws.On("disconnect", func() {
		websockfinished <- true
	})
	<-websockfinished
	log.Println("Websocket disconnected. Program finished")
	/////// testing & troubleshooting stuff after here
	// func() {
	// 	// test state with betting open
	// 	b := []byte(`{"p1name":"Nicholas d. wolfwood","p2name":"Axe pq","p1total":"0","p2total":"0","status":"open","alert":"","x":0,"remaining":"48 more matches until the next tournament!"}`)
	// 	log.Println(StateFrmBytes(b))
	// 	// test state with betting closed
	// 	b = []byte(`{"p1name":"Mike bison","p2name":"Grox","p1total":"2,733,397","p2total":"1,510,460","status":"locked","alert":"","x":1,"remaining":"45 more matches until the next tournament!"}`)
	// 	log.Println(StateFrmBytes(b))
	// }()
}
