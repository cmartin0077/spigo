// Package priamCassandra simulates a generic business logic microservice
// Takes incoming traffic and calls into dependent microservices in a single zone
package priamCassandra

import (
	"fmt"
	"github.com/adrianco/spigo/archaius"
	"github.com/adrianco/spigo/collect"
	"github.com/adrianco/spigo/gotocol"
	"log"
	"strings"
	"time"
)

// Start priamCassandra, all configuration and state is sent via messages
func Start(listener chan gotocol.Message) {
	dunbar := 30 // starting point for how many nodes to remember
	// remember the channel to talk to microservices
	microservices := make(map[string]chan gotocol.Message, dunbar)
	store := make(map[string]string, 4) // key value store
	store["why?"] = "because..."
	var netflixoss, requestor chan gotocol.Message // remember creator and how to talk back to incoming requests
	var name string                                // remember my name
	var edda chan gotocol.Message                  // if set, send updates
	var chatrate time.Duration
	hist := collect.NewHist("")
	chatTicker := time.NewTicker(time.Hour)
	chatTicker.Stop()
	for {
		select {
		case msg := <-listener:
			collect.Measure(hist, time.Since(msg.Sent))
			if archaius.Conf.Msglog {
				log.Printf("%v: %v\n", name, msg)
			}
			switch msg.Imposition {
			case gotocol.Hello:
				if name == "" {
					// if I don't have a name yet remember what I've been named
					netflixoss = msg.ResponseChan // remember how to talk to my namer
					name = msg.Intention          // message body is my name
					hist = collect.NewHist(name)
				}
			case gotocol.Inform:
				// remember where to send updates
				edda = msg.ResponseChan
				// logger channel is buffered so no need to use GoSend
				edda <- gotocol.Message{gotocol.Hello, nil, time.Now(), name + " " + "priamCassandra"}
			case gotocol.NameDrop:
				// don't remember too many buddies and don't talk to myself
				microservice := msg.Intention // message body is buddy name
				if len(microservices) < dunbar && microservice != name {
					// remember how to talk to this buddy
					microservices[microservice] = msg.ResponseChan // message channel is buddy's listener
					if edda != nil {
						// if it's setup, tell the logger I have a new buddy to talk to
						edda <- gotocol.Message{gotocol.Inform, listener, time.Now(), name + " " + microservice}
					}
				}
			case gotocol.Chat:
				// setup the ticker to run at the specified rate
				d, e := time.ParseDuration(msg.Intention)
				if e == nil && d >= time.Millisecond && d <= time.Hour {
					chatrate = d
					chatTicker = time.NewTicker(chatrate)
				}
			case gotocol.GetRequest:
				// return any stored value for this key (Cassandra READ.ONE behavior)
				gotocol.Message{gotocol.GetResponse, listener, time.Now(), store[msg.Intention]}.GoSend(msg.ResponseChan)
			case gotocol.GetResponse:
				// return path from a request, send payload back up (not currently used)
				if requestor != nil {
					gotocol.Message{gotocol.GetResponse, listener, time.Now(), msg.Intention}.GoSend(requestor)
				}
			case gotocol.Put:
				requestor = msg.ResponseChan
				// set a key value pair and replicate globally
				var key, value string
				fmt.Sscanf(msg.Intention, "%s%s", &key, &value)
				if key != "" && value != "" {
					store[key] = value
					// duplicate the request on to all connected priamCassandra nodes
					if len(microservices) > 0 {
						// replicate request
						for _, c := range microservices {
							gotocol.Message{gotocol.Replicate, listener, time.Now(), msg.Intention}.GoSend(c)
						}
					}
				}
			case gotocol.Replicate:
				// Replicate is only used between priamCassandra nodes
				// end point for a request
				var key, value string
				fmt.Sscanf(msg.Intention, "%s%s", &key, &value)
				// log.Printf("priamCassandra: %v:%v", key, value)
				if key != "" && value != "" {
					store[key] = value
				}
				// name looks like: netflixoss.us-east-1.zoneC.priamCassandra11
				myregion := strings.Split(name, ".")[1]
				//log.Printf("%v: %v\n", name, myregion)
				// find if this was a cross region Replicate
				for n, c := range microservices {
					// find the name matching incoming request channel
					if c == msg.ResponseChan {
						if myregion != strings.Split(n, ".")[1] {
							// Replicate from out of region needs to be Replicated only to other zones in this Region
							for nz, cz := range microservices {
								if myregion == strings.Split(nz, ".")[1] {
									//log.Printf("%v rep to: %v\n", name, nz)
									gotocol.Message{gotocol.Replicate, listener, time.Now(), msg.Intention}.GoSend(cz)
								}
							}
						}
					}
				}
			case gotocol.Goodbye:
				if archaius.Conf.Msglog {
					log.Printf("%v: Going away, zone: %v\n", name, store["zone"])
				}
				gotocol.Message{gotocol.Goodbye, nil, time.Now(), name}.GoSend(netflixoss)
				return
			}
		case <-chatTicker.C:
			// nothing to do here at the moment
		}
	}
}