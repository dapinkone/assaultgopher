package main

// credits:
// https://blog.alexellis.io/golang-json-api-client/
import (
	"encoding/json"

	"github.com/gocolly/colly/v2"
	"github.com/graarh/golang-socketio"
	"github.com/graarh/golang-socketio/transport"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func diaf(err error) {
	if err != nil {
		log.Fatal(err)

	}
}
func main() {
	// step 1: login
	c := colly.NewCollector()
	c.AllowURLRevisit = true

	// open up the auth.txt to get our username/password. TODO: need consult on more secure method.
	var file, err = os.OpenFile("./auth.txt", os.O_RDONLY, 0644)
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
		}, //TODO: stay logged in somehow?
	)
	diaf(err)

	// attach callbacks after login
	c.OnResponse(func(r *colly.Response) {
		log.Printf("rcvd %d %s", r.StatusCode, r.Request.URL)
	})

	// Check who's fighting.
	// get game state json from
	// https://www.saltybet.com/state.json
	// it's used over here https://www.saltybet.com/j/www-cdn-jtvnw-x.js
	type GameState struct {
		P1name  string // player 1/red's name
		P2name  string // player 2/blue's name
		P1total string // total $ on red, w/commas
		P2total string // total $ on blue, w/commas
		Status  string // betting open? "1", "2", "open" "locked"
		// 1		-p1's win.
		// 2		-p2's win.
		//  open	-betting open.
		// locked	-betting closed.
		Alert string
		// Tournament mode start!
		// Exhibition mode start!
		// ?? unknown
		// usually blank
		X int `json:"number"`
		//
		Remaining string
		// announcement of rounds until a tournament
		// announcement of characters left in tournament bracket
	}

	strToInt := func(s string) int {
		clean := strings.Replace(s, ",", "", -1)
		if clean != "" {
			i, err := strconv.Atoi(clean)
			diaf(err)
			return i
		} else {
			return 0
		}
	}

	calcOdds := func(p1total string, p2total string) float64 {
		return float64(strToInt(p1total)) / float64(strToInt(p2total))
	}

	currentBal := "1000" // default value?

	// c.OnHTML("#b", func(e *colly.HTMLElement) { // Why is this never showing up?
	// 	if e.Attr("value") != " " && e.Text != currentBal {
	// 		currentBal = e.Attr("value")
	// 		log.Println("Bal:", currentBal)
	// 	}
	// })
	uid := ""
	c.OnHTML("#u", func(e *colly.HTMLElement) { // this only shows up on startup.
		if e.Attr("value") != uid {
			uid = e.Attr("value")
			log.Println("Uid:", uid)
		}
	})

	StateFrmBytes := func(b []byte) GameState {
		s := GameState{}
		err := json.Unmarshal(b, &s)
		diaf(err)
		return s
	}

	prState := func(ts GameState) {
		odds := calcOdds(ts.P1total, ts.P2total)
		diaf(err)

		betstatus := func() string {
			switch ts.Status {
			case "1":
				return "Red Wins"
			case "2":
				return "Blue Wins"
			default:
				return "Bets are " + ts.Status
			}
		}()

		log.Printf("%s(%s)\tvs\t%s(%s)\t%s (%.2f:1)\tx:%d %s\tAlert: '%s'",
			ts.P1name, ts.P1total,
			ts.P2name, ts.P2total,
			betstatus, odds,

			ts.X, ts.Remaining, ts.Alert,
		)
	}

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
	var zData map[string]interface{}
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
			if err != nil || zData[uid] == nil {
				return // must not have placed a bet yet, or something went wrong with the json.
			}
			currentBal = (zData[uid].(map[string]interface{})["b"].(string)) // JSON BLACK MAGICKS
			log.Printf("Balance updated: %s", currentBal)
			if err != nil {
				return
			}
		}

	}
	updateZdata()
	lastStateBytes := []byte("")
	lastState := GameState{}
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
			prState(lastState)
			lastStateBytes = body
			diaf(err)

			if lastState.Status == "open" {
				updateZdata() // Zdata will update our balance and whatnots.
				// place bet!
				// post request to /ajax_place_bet.php
				// for now, always bet red(player1)
				wager := strconv.Itoa(strToInt(currentBal) / 10)
				c.Post(
					"https://www.saltybet.com/ajax_place_bet.php",
					map[string]string{
						"selectedplayer": "player1",
						"wager":          wager,
					},
				)
				log.Printf("Bet placed for %s on %s!", wager, lastState.P1name)

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
		// socket receive: type "open", data "{"sid":"36Q3vIX320gzWmcFBMtO","upgrades":["websocket"],"pingInterval":25000,"pingTimeout":60000}" +1ms

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
	func() {
		// test state with betting open
		b := []byte(`{"p1name":"Nicholas d. wolfwood","p2name":"Axe pq","p1total":"0","p2total":"0","status":"open","alert":"","x":0,"remaining":"48 more matches until the next tournament!"}`)
		prState(StateFrmBytes(b))
		// test state with betting closed
		b = []byte(`{"p1name":"Mike bison","p2name":"Grox","p1total":"2,733,397","p2total":"1,510,460","status":"locked","alert":"","x":1,"remaining":"45 more matches until the next tournament!"}`)
		prState(StateFrmBytes(b))
	}()
}
