package main

// credits:
// https://blog.alexellis.io/golang-json-api-client/

import (
	"encoding/json"
	"fmt"
	"github.com/asdine/storm"
	"github.com/gocolly/colly/v2"
	gosocketio "github.com/graarh/golang-socketio"
	"github.com/graarh/golang-socketio/transport"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func strToInt(s string) int {
	clean := strings.Replace(s, ",", "", -1)
	if clean != "" {
		i, err := strconv.Atoi(clean)
		diaf(err)
		return i
	} else {
		return 0
	}
}

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

func (g *GameState) calcOdds() float64 {
	return float64(strToInt(g.P1total)) / float64(strToInt(g.P2total))
}

func (g GameState) String() string {
	/* implements the stringer interface on GameState, so we can use standard print stuff on it
	   with our desired custom output */
	odds := g.calcOdds()
	betstatus := func() string {
		switch g.Status {
		case "1":
			return "Red Wins"
		case "2":
			return "Blue Wins"
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

	currentBal := "1000" // default value?

	c.OnHTML("#b", func(e *colly.HTMLElement) { // Why is this never showing up?
		if e.Attr("value") != " " && e.Text != currentBal {
			currentBal = e.Attr("value")
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
	var zDataBytes = []byte("")
	var zData map[string]json.RawMessage

	updateZdata := func() { // TODO: refactor updateZdata + updateState?
		// fetch zdata json
		httpClient := http.Client{Timeout: time.Second * 10}

		req, err := http.NewRequest(
			http.MethodGet,
			"https://www.saltybet.com/zdata.json?t="+t(),
			nil,
		)
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

		// if the most recent json bytes != last json bytes, update using new bytes.
		if string(body) != string(zDataBytes) {
			err = json.Unmarshal(body, &zData)
			if zData[uid] == nil {
				return // we haven't bet
			}
			if err != nil {
				log.Println(" -- Error: ", err)
				return // something went wrong with the json.
			}
			var userzData map[string]string
			err = json.Unmarshal(zData[uid], &userzData)
			if err != nil {
				log.Println(" -- Error: ", err)
				return
			}

			profit := strToInt(userzData["b"]) - strToInt(currentBal)
			currentBal = userzData["b"]
			log.Printf("Balance updated: %s Change: %d", currentBal, profit)
		}
	}
	updateZdata() // we do all that just for the balance?
	lastStateBytes := []byte("")
	lastState := GameState{}
	wager := 0
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

		// if the most recent json bytes != last state json, update lastState using new bytes.
		if string(body) != string(lastStateBytes) {
			err = json.Unmarshal(body, &lastState)
			log.Println(lastState)
			lastStateBytes = body
			diaf(err)

			switch lastState.Status {
			case "open":
				{
					updateZdata() // Zdata will update our balance and whatnots.
					// place bet!
					// post request to /ajax_place_bet.php
					// for now, always bet red(player1)
					wager = strToInt(currentBal) / 10
					c.Post(
						"https://www.saltybet.com/ajax_place_bet.php",
						map[string]string{
							"selectedplayer": "player1",
							"wager":          strconv.Itoa(wager),
						},
					)
					log.Printf("Bet placed for %d on %s!", wager, lastState.P1name)
					break
				}
			case "locked":
				// bets are now closed.
			case "1", "2": // fight's over.
				{
					err = db.Save(&fightHist{
						Time:    int(time.Now().Unix()),
						P1name:  lastState.P1name,
						P2name:  lastState.P2name,
						P1total: strToInt(lastState.P1total),
						P2total: strToInt(lastState.P2total),
						Winner:  lastState.Status,
						Bet:     0, // TODO: put the bet data in when you calculate profits later.
						Profit:  0,
						X:       lastState.X,
					})
					if err != nil {
						log.Println(err)
					}
					var hist []fightHist
					err = db.All(&hist)
					if err != nil {
						log.Println(err)
					}
					log.Printf("%d records.", len(hist))

				}
			}

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

	for { // wait forever.
		time.Sleep(time.Second * 30)
	}

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
